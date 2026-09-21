package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 验证实际 HTTP 请求，防止把业务 token 误当作 IM token 或提交错误的密码格式。
func TestLoginByPhone(t *testing.T) {
	for _, mode := range []string{"md5", "plain"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/account/login" || r.Header.Get("operationID") == "" {
					t.Errorf("登录请求不正确：%s %s", r.Method, r.URL.Path)
				}
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				wantPassword := "test123456"
				if mode == "md5" {
					wantPassword = "47ec2dd791e31e2ef2076caf64ed9b3d"
				}
				if req["password"] != wantPassword || req["phoneNumber"] != "13800138000" || req["areaCode"] != "+86" || req["platform"] != float64(8) {
					t.Error("手机号、区号、平台或密码编码不符合协议")
				}
				fmt.Fprint(w, `{"errCode":0,"errMsg":"","errDlt":"","data":{"userID":"alice","imToken":"im-token","chatToken":"chat-token"}}`)
			}))
			defer server.Close()
			got, err := loginByPhone(context.Background(), server.Client(), server.URL, phoneCredentials{"+86", "13800138000", "test123456", mode, 8})
			if err != nil {
				t.Fatal(err)
			}
			if got.UserID != "alice" || got.IMToken != "im-token" || got.ChatToken != "chat-token" {
				t.Fatalf("错误的登录结果：%+v", got)
			}
		})
	}
}

// 业务失败、代理错误和不完整响应必须阻止后续 SDK 登录。
func TestLoginRejectsInvalidResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"密码错误", 200, `{"errCode":10002,"errMsg":"password error"}`},
		{"缺少IMToken", 200, `{"errCode":0,"data":{"userID":"alice","chatToken":"chat"}}`},
		{"缺少用户ID", 200, `{"errCode":0,"data":{"imToken":"im"}}`},
		{"缺少状态码", 200, `{"data":{"userID":"alice","imToken":"im"}}`},
		{"无效JSON", 200, `<html>gateway error</html>`},
		{"HTTP失败", 502, `bad gateway`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			_, err := loginByPhone(context.Background(), server.Client(), server.URL, phoneCredentials{"+86", "13800138000", "secret", "md5", 8})
			if err == nil {
				t.Fatal("应拒绝失败或不完整响应")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("错误信息不应包含密码")
			}
		})
	}
}
