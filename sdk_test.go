package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 单独收到连接事件或同步事件都不能进入聊天，避免第一次启动显示空好友列表。
func TestWaitReadyNeedsConnectionAndSync(t *testing.T) {
	for _, tc := range []struct {
		name              string
		connected, synced bool
		ready             bool
	}{
		{"只有连接", true, false, false},
		{"只有同步", false, true, false},
		{"连接和同步完成", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := &sdkListener{connected: make(chan struct{}), synced: make(chan struct{})}
			if tc.connected {
				listener.OnConnectSuccess()
				listener.OnConnectSuccess()
			}
			if tc.synced {
				listener.OnSyncServerFinish(true)
				listener.OnSyncServerFinish(false)
			}
			err := waitReady(context.Background(), listener, 20*time.Millisecond)
			if tc.ready && err != nil {
				t.Fatal(err)
			}
			if !tc.ready && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("应等待超时，得到 %v", err)
			}
		})
	}
}

// 超时只是停止等待，SDK 之后仍可能回调；晚到和重复回调必须可以安全返回。
func TestCallbackAfterTimeoutDoesNotBlock(t *testing.T) {
	var callback *sdkCallback
	_, err := callSDK(context.Background(), time.Millisecond, func(cb *sdkCallback) { callback = cb })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("期望超时，得到 %v", err)
	}
	done := make(chan struct{})
	go func() { callback.OnSuccess("ok"); callback.OnError(1, "late"); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("晚到的回调阻塞")
	}
}

func TestCallbackErrorIsReturned(t *testing.T) {
	_, err := callSDK(context.Background(), time.Second, func(cb *sdkCallback) { cb.OnError(1001, "发送失败") })
	if err == nil {
		t.Fatal("不能将 SDK 的失败回调当作成功")
	}
}
