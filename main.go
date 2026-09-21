// 本示例演示：手机号/密码登录 → 打印 token → IM 登录 → 好友列表 → 单聊收发。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/openimsdk/openim-sdk-core/v3/open_im_sdk"
	"github.com/openimsdk/openim-sdk-core/v3/sdk_struct"
	"github.com/openimsdk/protocol/constant"
)

type options struct {
	chatURL, apiURL, wsURL, dataDir string
	credentials                     phoneCredentials
	timeout                         time.Duration
	loginOnly                       bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败：", err)
		os.Exit(1)
	}
}

func readOptions() (options, error) {
	return readOptionsFrom(os.Args[1:], os.Getenv, "client.conf")
}

func readOptionsFrom(args []string, getenv func(string) string, defaultConfigPath string) (options, error) {
	var o options
	phone, password := getenv("OPENIM_PHONE"), getenv("OPENIM_PASSWORD")
	chatURL, apiURL, wsURL := "http://127.0.0.1:10008", "http://127.0.0.1:10002", "ws://127.0.0.1:10001"
	configPath, loadConfig, err := configFileForArgs(args, defaultConfigPath)
	if err != nil {
		return o, err
	}
	if loadConfig {
		config, err := loadClientConfig(configPath)
		if err != nil {
			return o, err
		}
		phone, password = config["OPENIM_PHONE"], config["OPENIM_PASSWORD"]
		chatURL, apiURL, wsURL = config["chat-url"], config["api-url"], config["ws-url"]
	}

	flags := flag.NewFlagSet("openim_client_v1", flag.ContinueOnError)
	flags.StringVar(&configPath, "c", configPath, "配置文件路径（不传任何参数时默认读取 client.conf）")
	flags.StringVar(&o.chatURL, "chat-url", chatURL, "openim-chat 业务服务地址")
	flags.StringVar(&o.apiURL, "api-url", apiURL, "OpenIM API 地址")
	flags.StringVar(&o.wsURL, "ws-url", wsURL, "OpenIM WebSocket 地址")
	flags.StringVar(&o.credentials.PhoneNumber, "phone", phone, "手机号（也可用 OPENIM_PHONE）")
	flags.StringVar(&o.credentials.AreaCode, "area-code", "+86", "国际区号")
	flags.StringVar(&o.credentials.PasswordMode, "password-mode", "md5", "密码提交格式：md5 或 plain")
	flags.IntVar(&o.credentials.Platform, "platform", defaultPlatform(), "平台 ID，必须与签发 imToken 的平台一致")
	flags.StringVar(&o.dataDir, "data-dir", "./data", "SDK 数据库和日志目录，请勿多进程共用")
	flags.DurationVar(&o.timeout, "timeout", 60*time.Second, "请求及首次同步的等待时间")
	flags.BoolVar(&o.loginOnly, "login-only", false, "只进行业务登录并打印 token")
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	// 密码只从环境变量或配置文件读取，不写入源码，也不放进命令行参数。
	o.credentials.Password = password
	if strings.TrimSpace(o.credentials.PhoneNumber) == "" || o.credentials.Password == "" {
		return o, fmt.Errorf("请在配置文件中设置 OPENIM_PHONE 和 OPENIM_PASSWORD，或通过 -phone 与 OPENIM_PASSWORD 指定账号；用 -h 查看帮助")
	}
	if o.timeout <= 0 {
		return o, fmt.Errorf("timeout 必须大于 0")
	}
	if strings.TrimSpace(o.credentials.AreaCode) == "" {
		return o, fmt.Errorf("area-code 不能为空")
	}
	if o.credentials.PasswordMode != "md5" && o.credentials.PasswordMode != "plain" {
		return o, fmt.Errorf("password-mode 只支持 md5 或 plain")
	}
	if _, ok := constant.PlatformID2Name[o.credentials.Platform]; !ok {
		return o, fmt.Errorf("无效的 platform：%d", o.credentials.Platform)
	}
	for _, endpoint := range []struct {
		name, address string
		websocket     bool
	}{
		{"chat-url", o.chatURL, false}, {"api-url", o.apiURL, false}, {"ws-url", o.wsURL, true},
	} {
		u, err := url.Parse(endpoint.address)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return o, fmt.Errorf("%s 必须是有效的服务地址，不含用户信息、查询参数或片段", endpoint.name)
		}
		if endpoint.websocket {
			if u.Scheme != "ws" && u.Scheme != "wss" {
				return o, fmt.Errorf("ws-url 必须使用 ws 或 wss")
			}
		} else if u.Scheme != "http" && u.Scheme != "https" {
			return o, fmt.Errorf("%s 必须使用 http 或 https", endpoint.name)
		}
	}
	return o, nil
}

func configFileForArgs(args []string, defaultPath string) (string, bool, error) {
	if len(args) == 0 {
		return defaultPath, true, nil
	}
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--":
			return defaultPath, false, nil
		case args[i] == "-c":
			if i+1 == len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", false, fmt.Errorf("-c 必须指定配置文件路径")
			}
			return args[i+1], true, nil
		case strings.HasPrefix(args[i], "-c="):
			path := strings.TrimSpace(strings.TrimPrefix(args[i], "-c="))
			if path == "" {
				return "", false, fmt.Errorf("-c 必须指定配置文件路径")
			}
			return path, true, nil
		}
	}
	return defaultPath, false, nil
}

func loadClientConfig(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %q：%w", path, err)
	}
	defer file.Close()

	config := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" {
			return nil, fmt.Errorf("配置文件 %q 第 %d 行格式错误，应为 key=value", path, lineNumber)
		}
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		switch key {
		case "OPENIM_PHONE", "OPENIM_PASSWORD", "chat-url", "api-url", "ws-url":
			config[key] = value
		default:
			return nil, fmt.Errorf("配置文件 %q 第 %d 行包含未知配置项 %q", path, lineNumber, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取配置文件 %q：%w", path, err)
	}
	return config, nil
}

func defaultPlatform() int {
	switch runtime.GOOS {
	case "darwin":
		return constant.OSXPlatformID
	case "windows":
		return constant.WindowsPlatformID
	default:
		return constant.LinuxPlatformID
	}
}

func run() error {
	o, err := readOptions()
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	// Ctrl+C 和 SIGTERM 可以打断网络等待及交互输入。
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancelCause(signalCtx)
	defer cancel(nil)
	client := &http.Client{
		Timeout: o.timeout,
		// 登录接口不需要重定向，避免密码请求被转发到其他地址。
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	result, err := loginByPhone(ctx, client, o.chatURL, o.credentials)
	if err != nil {
		return err
	}
	// 按示例需求输出完整 token；成功后才输出，不输出密码。
	fmt.Printf("业务登录成功\nuserID: %s\nimToken: %s\nchatToken: %s\n", result.UserID, result.IMToken, result.ChatToken)
	if o.loginOnly {
		return nil
	}

	dataDir, err := filepath.Abs(o.dataDir)
	if err != nil {
		return err
	}
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("创建数据目录：%w", err)
	}
	config, err := json.Marshal(sdk_struct.IMConfig{
		PlatformID: int32(o.credentials.Platform), SystemType: runtime.GOOS,
		ApiAddr: strings.TrimRight(o.apiURL, "/"), WsAddr: strings.TrimRight(o.wsURL, "/"),
		DataDir: dataDir, LogFilePath: logDir, LogLevel: 2, IsLogStandardOutput: false,
	})
	if err != nil {
		return err
	}
	listener := &sdkListener{connected: make(chan struct{}), synced: make(chan struct{}), cancel: cancel}
	if !open_im_sdk.InitSDK(listener, operationID(), string(config)) {
		return fmt.Errorf("SDK 初始化失败")
	}
	defer open_im_sdk.UnInitSDK(operationID())
	open_im_sdk.SetConversationListener(listener)
	open_im_sdk.SetAdvancedMsgListener(listener)
	_, err = callSDK(ctx, o.timeout, func(cb *sdkCallback) {
		open_im_sdk.Login(cb, operationID(), result.UserID, result.IMToken)
	})
	if err != nil {
		return fmt.Errorf("IM 登录：%w", err)
	}
	defer func() {
		// 主 context 可能已取消，退出登录使用独立的有限等待。
		_, err := callSDK(context.Background(), 25*time.Second, func(cb *sdkCallback) { open_im_sdk.Logout(cb, operationID()) })
		if err != nil {
			fmt.Fprintln(os.Stderr, "退出 SDK：", err)
		}
	}()
	if err := waitReady(ctx, listener, o.timeout); err != nil {
		return err
	}
	fmt.Println("IM 已连接，同步完成。")
	return chat(ctx, o.timeout)
}

// 输入单独读取，保证被踢下线或按 Ctrl+C 时主流程不阻塞在终端输入上。
func readLines(ctx context.Context) <-chan string {
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "读取输入失败：", err)
		}
	}()
	return lines
}

func nextLine(ctx context.Context, lines <-chan string) (string, error) {
	select {
	case <-ctx.Done():
		return "", context.Cause(ctx)
	case line, ok := <-lines:
		if !ok {
			return "", io.EOF
		}
		return line, nil
	}
}

func chat(ctx context.Context, timeout time.Duration) error {
	lines := readLines(ctx)
	for {
		friends, err := listFriends(ctx, timeout)
		if err != nil {
			return err
		}
		fmt.Printf("\n全部好友（%d 人）：\n", len(friends))
		for i, f := range friends {
			fmt.Printf("%d. %s  userID=%s  备注=%s\n", i+1, f.Nickname, f.UserID, f.Remark)
		}
		if len(friends) == 0 {
			fmt.Println("目前没有好友，请先在官方客户端添加好友后再运行。")
			return nil
		}
		fmt.Print("输入好友编号开始聊天，/quit 退出： ")
		line, err := nextLine(ctx, lines)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "/quit" {
			return nil
		}
		number, err := strconv.Atoi(line)
		if err != nil || number < 1 || number > len(friends) {
			fmt.Println("请输入列表中的有效编号。")
			continue
		}
		selected := friends[number-1]
		fmt.Printf("正在与 %s (%s) 聊天。输入文本发送，/friends 重新选择好友，/quit 退出。\n", selected.Nickname, selected.UserID)
		for {
			fmt.Print("> ")
			text, err := nextLine(ctx, lines)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			switch strings.TrimSpace(text) {
			case "/quit":
				return nil
			case "/friends":
			case "":
				continue
			default:
				if err := sendText(ctx, timeout, selected.UserID, text); err != nil {
					fmt.Fprintln(os.Stderr, "发送未确认成功：", err, "（超时后仍可能送达，请先核实再重发）")
				} else {
					fmt.Println("已发送。")
				}
				continue
			}
			break
		}
	}
}
