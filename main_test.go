package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 如果删除“零参数时读取配置”的分支，本测试必须失败。
func TestReadOptionsLoadsClientConfWithoutArguments(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "client.conf")
	content := "OPENIM_PHONE='13800138000'\n" +
		"OPENIM_PASSWORD='secret'\n" +
		"chat-url=http://192.168.13.1:10008\n" +
		"api-url=http://192.168.13.1:10002\n" +
		"ws-url=ws://192.168.13.1:10001\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := readOptionsFrom(nil, func(string) string { return "" }, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.credentials.PhoneNumber != "13800138000" || got.credentials.Password != "secret" {
		t.Fatalf("账号配置未加载：phone=%q passwordSet=%t", got.credentials.PhoneNumber, got.credentials.Password != "")
	}
	if got.chatURL != "http://192.168.13.1:10008" || got.apiURL != "http://192.168.13.1:10002" || got.wsURL != "ws://192.168.13.1:10001" {
		t.Fatalf("服务地址配置未加载：chat=%q api=%q ws=%q", got.chatURL, got.apiURL, got.wsURL)
	}
}

// 如果 -c 没有决定实际读取的配置文件，本测试必须失败。
func TestReadOptionsLoadsSpecifiedConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom.conf")
	content := "OPENIM_PHONE=13900139000\n" +
		"OPENIM_PASSWORD=custom-secret\n" +
		"chat-url=http://10.0.0.8:10008\n" +
		"api-url=http://10.0.0.8:10002\n" +
		"ws-url=ws://10.0.0.8:10001\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := readOptionsFrom([]string{"-c", configPath}, func(string) string { return "" }, "unused.conf")
	if err != nil {
		t.Fatal(err)
	}
	if got.credentials.PhoneNumber != "13900139000" || got.credentials.Password != "custom-secret" || got.apiURL != "http://10.0.0.8:10002" {
		t.Fatalf("未加载 -c 指定的配置：phone=%q passwordSet=%t api=%q", got.credentials.PhoneNumber, got.credentials.Password != "", got.apiURL)
	}
}
