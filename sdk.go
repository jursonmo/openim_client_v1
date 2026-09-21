package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/openimsdk/openim-sdk-core/v3/open_im_sdk"
	"github.com/openimsdk/openim-sdk-core/v3/open_im_sdk_callback"
	"github.com/openimsdk/openim-sdk-core/v3/pkg/ccontext"
	"github.com/openimsdk/openim-sdk-core/v3/sdk_struct"
)

type sdkResult struct {
	data string
	err  error
}

// SDK 通过异步回调返回结果；容量为 1 的通道保证超时后的回调不会阻塞。
type sdkCallback struct {
	once   sync.Once
	result chan sdkResult
}

func (c *sdkCallback) OnSuccess(data string) { c.complete(sdkResult{data: data}) }
func (c *sdkCallback) OnError(code int32, message string) {
	c.complete(sdkResult{err: fmt.Errorf("SDK 错误 %d：%s", code, message)})
}
func (c *sdkCallback) OnProgress(_ int)          {} // 文本消息无需显示上传进度。
func (c *sdkCallback) complete(result sdkResult) { c.once.Do(func() { c.result <- result }) }

func callSDK(ctx context.Context, timeout time.Duration, start func(*sdkCallback)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", context.Cause(ctx)
	}
	callback := &sdkCallback{result: make(chan sdkResult, 1)}
	start(callback)
	select {
	case result := <-callback.result:
		return result.data, result.err
	case <-ctx.Done():
		return "", context.Cause(ctx)
	}
}

// 同时监听连接、首次同步和消息，必须在 Login 之前注册。
type sdkListener struct {
	connected, synced     chan struct{}
	connectOnce, syncOnce sync.Once
	cancel                context.CancelCauseFunc
}

var (
	_ open_im_sdk_callback.OnConnListener         = (*sdkListener)(nil)
	_ open_im_sdk_callback.OnConversationListener = (*sdkListener)(nil)
	_ open_im_sdk_callback.OnAdvancedMsgListener  = (*sdkListener)(nil)
	_ open_im_sdk_callback.SendMsgCallBack        = (*sdkCallback)(nil)
)

func (l *sdkListener) OnConnecting()     { fmt.Fprintln(os.Stderr, "正在连接 IM 服务……") }
func (l *sdkListener) OnConnectSuccess() { l.connectOnce.Do(func() { close(l.connected) }) }
func (l *sdkListener) OnConnectFailed(code int32, message string) {
	fmt.Fprintf(os.Stderr, "IM 连接失败，SDK 将重试：%d %s\n", code, message)
}
func (l *sdkListener) OnKickedOffline() { l.cancel(fmt.Errorf("当前账号被踢下线")) }
func (l *sdkListener) OnUserTokenExpired() {
	l.cancel(fmt.Errorf("imToken 已过期，请重新登录"))
}
func (l *sdkListener) OnUserTokenInvalid(message string) {
	l.cancel(fmt.Errorf("imToken 无效：%s", message))
}
func (l *sdkListener) OnSyncServerStart(_ bool) {
	fmt.Fprintln(os.Stderr, "正在同步好友和会话……")
}
func (l *sdkListener) OnSyncServerFinish(_ bool) { l.syncOnce.Do(func() { close(l.synced) }) }
func (l *sdkListener) OnSyncServerFailed(_ bool) {
	l.cancel(fmt.Errorf("同步失败，暂时无法确认完整好友列表"))
}
func (l *sdkListener) OnSyncServerProgress(_ int)                    {}
func (l *sdkListener) OnNewConversation(_ string)                    {}
func (l *sdkListener) OnConversationChanged(_ string)                {}
func (l *sdkListener) OnTotalUnreadMessageCountChanged(_ int32)      {}
func (l *sdkListener) OnConversationUserInputStatusChanged(_ string) {}

func (l *sdkListener) OnRecvNewMessage(message string)        { printMessage("新消息", message) }
func (l *sdkListener) OnRecvOfflineNewMessage(message string) { printMessage("离线消息", message) }
func (l *sdkListener) OnRecvOnlineOnlyMessage(message string) { printMessage("在线消息", message) }
func (l *sdkListener) OnRecvC2CReadReceipt(_ string)          {}
func (l *sdkListener) OnNewRecvMessageRevoked(_ string)       {}
func (l *sdkListener) OnMsgDeleted(_ string)                  {}
func (l *sdkListener) OnMessageModified(_ string)             {}

func printMessage(label, raw string) {
	var message sdk_struct.MsgStruct
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		fmt.Fprintln(os.Stderr, "收到消息，但解析失败：", err)
		return
	}
	if message.TextElem != nil {
		fmt.Printf("\n[%s] %s：%s\n", label, message.SendID, message.TextElem.Content)
	} else {
		fmt.Printf("\n[%s] %s：非文本消息（类型 %d）\n", label, message.SendID, message.ContentType)
	}
}

func waitReady(ctx context.Context, listener *sdkListener, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Login 的成功回调仅代表本地初始化完成，不能据此认定 WebSocket 已连接。
	for _, ready := range []<-chan struct{}{listener.connected, listener.synced} {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 IM 连接/同步：%w", context.Cause(ctx))
		case <-ready:
		}
	}
	return nil
}

type friend struct {
	UserID   string `json:"userID"`
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
}

func listFriends(ctx context.Context, timeout time.Duration) ([]friend, error) {
	// 复用旧数据库登录时，SDK 的好友同步是异步的，可能晚于会话同步事件。
	// Go 示例可直接调用 SDK 导出的同步方法，明确等待完成并检查错误后再读取。
	syncCtx, cancel := context.WithTimeout(ccontext.WithOperationID(
		ccontext.WithInfo(ctx, open_im_sdk.IMUserContext.Info()), operationID()), timeout)
	defer cancel()
	synced := make(chan error, 1)
	go func() { synced <- open_im_sdk.IMUserContext.Relation().IncrSyncFriendsWithLock(syncCtx) }()
	select {
	case err := <-synced:
		if err != nil {
			return nil, fmt.Errorf("同步好友列表：%w", err)
		}
	case <-syncCtx.Done():
		return nil, fmt.Errorf("等待好友同步：%w", context.Cause(syncCtx))
	}
	data, err := callSDK(ctx, timeout, func(cb *sdkCallback) {
		// false 表示不排除黑名单中的好友；此接口返回完整列表，无需分页。
		open_im_sdk.GetFriendList(cb, operationID(), false)
	})
	if err != nil {
		return nil, err
	}
	var friends []friend
	if err := json.Unmarshal([]byte(data), &friends); err != nil {
		return nil, fmt.Errorf("解析好友列表：%w", err)
	}
	return friends, nil
}

func sendText(ctx context.Context, timeout time.Duration, userID, text string) error {
	message := open_im_sdk.CreateTextMessage(operationID(), text)
	if message == "" {
		return fmt.Errorf("创建文本消息失败")
	}
	_, err := callSDK(ctx, timeout, func(cb *sdkCallback) {
		// 单聊填写 recvID，groupID 留空；false 表示保存历史，支持离线接收。
		// 当前 SDK 异步发送入队不代表成功，必须等 OnSuccess/OnError 回调。
		open_im_sdk.SendMessage(cb, operationID(), message, userID, "", `{"title":"新消息","desc":"你收到一条消息"}`, false)
	})
	return err
}
