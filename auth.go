package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type phoneCredentials struct {
	AreaCode, PhoneNumber, Password, PasswordMode string
	Platform                                      int
}

// chatToken 用于业务服务，imToken 用于 IM SDK，二者不能混用。
type loginResult struct {
	UserID    string `json:"userID"`
	IMToken   string `json:"imToken"`
	ChatToken string `json:"chatToken"`
}

var operationSequence atomic.Uint64

// 每次调用使用独立的 operationID，方便在服务端定位请求。
func operationID() string {
	return fmt.Sprintf("go-client-%d-%d", time.Now().UnixNano(), operationSequence.Add(1))
}

func loginByPhone(ctx context.Context, client *http.Client, chatURL string, credentials phoneCredentials) (loginResult, error) {
	var result loginResult
	password := credentials.Password
	switch credentials.PasswordMode {
	case "md5":
		// 与官方 Electron 客户端一致：提交密码的 MD5 小写十六进制值。
		// 这是服务端兼容协议，并非安全的密码存储方案，远程连接应使用 HTTPS。
		password = fmt.Sprintf("%x", md5.Sum([]byte(password)))
	case "plain":
		// 仅用于直接通过 API 以明文密码注册的测试账号，不自动尝试另一种格式。
	default:
		return result, fmt.Errorf("password-mode 只支持 md5 或 plain")
	}
	body, err := json.Marshal(struct {
		AreaCode    string `json:"areaCode"`
		PhoneNumber string `json:"phoneNumber"`
		Password    string `json:"password"`
		Platform    int    `json:"platform"`
	}{credentials.AreaCode, credentials.PhoneNumber, password, credentials.Platform})
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(chatURL, "/")+"/account/login", bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("创建登录请求：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("operationID", operationID())
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("请求 openim-chat：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("登录 HTTP 状态：%d", resp.StatusCode)
	}
	var envelope struct {
		ErrCode *int        `json:"errCode"`
		ErrMsg  string      `json:"errMsg"`
		Data    loginResult `json:"data"`
	}
	// 限制响应大小，错误时不打印原始响应或请求中的密码。
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return result, fmt.Errorf("解析登录响应失败：%w", err)
	}
	if envelope.ErrCode == nil {
		return result, fmt.Errorf("登录响应缺少 errCode，请检查 chat 服务地址")
	}
	if *envelope.ErrCode != 0 {
		return result, fmt.Errorf("业务登录失败，errCode=%d，errMsg=%s", *envelope.ErrCode, envelope.ErrMsg)
	}
	if envelope.Data.UserID == "" || envelope.Data.IMToken == "" {
		return result, fmt.Errorf("登录响应缺少 userID 或 imToken")
	}
	return envelope.Data, nil
}
