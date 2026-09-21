# OpenIM Go 命令行客户端示例

包含中文注释，演示以下完整流程：

1. 使用区号、手机号和密码调用官方 openim-chat 的 `POST /account/login`。
2. 成功后打印 `userID`、`imToken` 和 `chatToken`。
3. 使用 `userID + imToken` 登录本地 `openim-sdk-core`，等待连接和同步完成。
4. 同步并列出全部好友，输入编号选择好友。
5. 循环发送文本消息，同时显示收到的新消息和离线消息。

## 环境准备

- Go 1.24 或以上，以及 C 编译器；SDK 使用 SQLite，须启用 CGO（通常默认开启）。macOS 可使用 Xcode Command Line Tools，Linux 可使用 GCC。
- 已部署且相互连通的官方 openim-chat 和 open-im-server。
- 已在业务服务注册的手机号账号。聊天演示需要至少一个已经建立好友关系的账号。
- 保持目录相邻，`go.mod` 通过 `replace` 使用本地 SDK：

```text
openim/
├── open-im-server/
├── openim-sdk-core/
└── openim_client_v1/
```

客户端不依赖服务端源码参与编译。首次执行 Go 命令需要联网下载 SDK 的依赖。

## 运行

在本目录运行，把示例手机号和服务地址换成自己的值：

```bash
export OPENIM_PHONE='13800138000'
export OPENIM_PASSWORD='你的登录密码'

go run . \
  -chat-url http://127.0.0.1:10008 \
  -api-url http://127.0.0.1:10002 \
  -ws-url ws://127.0.0.1:10001

  go run . \
  -chat-url http://192.168.4.200:10008 \
  -api-url http://192.168.4.200:10002 \
  -ws-url ws://192.168.4.200:10001
```

密码只从 `OPENIM_PASSWORD` 读取。也可以在终端隐藏输入密码，避免将密码写进历史：

```bash
# macOS 默认 zsh
read -s 'OPENIM_PASSWORD?请输入登录密码：'; echo
export OPENIM_PASSWORD
go run . -phone 13800138000
```

成功后的输出形式如下（token 为占位示例）：

```text
业务登录成功
userID: 1234567890
imToken: <IM 登录 token>
chatToken: <业务服务 token>
...
全部好友（2 人）：
1. 张三  userID=friend001  备注=同事
2. 李四  userID=friend002  备注=
输入好友编号开始聊天，/quit 退出： 1
正在与 张三 (friend001) 聊天。
> 你好，这是 Go 客户端发来的消息！
已发送。
[新消息] friend001：收到！
```

输入 `/friends` 重新同步、列出好友并选择聊天对象；输入 `/quit` 或按 Ctrl+C 退出。程序会尝试退出 SDK；网络异常时清理可能需要约 20 秒。收到的消息显示发送者 userID，包括其他会话的新消息。

只验证业务登录并打印 token，不连接 IM：

```bash
go run . -phone 13800138000 -login-only
```

构建可执行文件：

```bash
go build -o openim_client_v1 .
./openim_client_v1 -phone 13800138000
```

## 参数与密码格式

执行 `go run . -h` 查看中文帮助。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-chat-url` | `http://127.0.0.1:10008` | 官方业务服务，不是 IM API |
| `-api-url` | `http://127.0.0.1:10002` | IM HTTP API |
| `-ws-url` | `ws://127.0.0.1:10001` | IM WebSocket |
| `-phone` | `OPENIM_PHONE` | 手机号，不含国际区号 |
| `-area-code` | `+86` | 国际区号 |
| `-platform` | 跟随操作系统 | macOS=4、Windows=3、Linux=7，业务登录与 SDK 使用同一值 |
| `-password-mode` | `md5` | 见下方说明 |
| `-data-dir` | `./data` | 本地 SQLite 数据和 SDK 日志 |
| `-timeout` | `60s` | 请求及同步等待时间 |
| `-login-only` | `false` | 仅打印业务登录结果 |

官方 Electron 客户端在提交密码前做一次 MD5，因此此示例默认对 `OPENIM_PASSWORD` 中的原始密码做一次 MD5。请不要提前把环境变量设成 MD5 值。

如果账号通过 API 直接以明文密码注册（例如 open-im-server 的 CI 示例），使用 `-password-mode plain` 原样提交。程序不会在登录失败后自动改用其他密码格式。

`imToken` 用于 IM SDK；`chatToken` 用于 openim-chat 的业务接口。按本示例需求，两个 token 会完整输出到终端。远程服务请使用 HTTPS/WSS。

运行多个客户端时，为每个进程指定不同的 `-data-dir`。同一账号同一平台的登录可能根据服务端策略互踢，演示双方对话时使用两个已经互为好友的账号。

## 代码入口与验证

- `main.go`：参数、业务登录、打印 token、SDK 生命周期、好友选择和交互循环。
- `auth.go`：官方手机号密码 HTTP 登录和响应检查。
- `sdk.go`：SDK 回调、连接与同步等待、完整好友列表、文本收发。
- `auth_test.go`：通过本地 HTTP 测试服务验证请求格式、密码编码、token 解析和失败响应。
- `sdk_test.go`：验证连接/同步等待，以及超时、重复和错误回调。

```bash
go test ./...
go vet ./...
```

这些测试不连接真实账号。真实收发验证：分别在两个终端用不同账号和数据目录启动程序，互相发送文本并确认接收输出。发送超时仅意味着客户端停止等待，消息仍可能送达，重发前先核实。

## 对照源码

- 本地 `openim-sdk-core/open_im_sdk/init_login.go`、`relation.go`、`conversation_msg.go`。
- 本地 `open-im-server/.github/workflows/go-build-test.yml` 的手机号登录请求示例。
- [官方 chat HTTP 登录实现](https://github.com/openimsdk/chat/blob/73e7b82a64fdc997a415f3a55f697e0c5800f1d1/internal/api/chat/chat.go)：返回 `imToken`、`chatToken` 和 `userID`。
- [官方 Electron 密码登录](https://github.com/openimsdk/openim-electron-demo/blob/62d7ca7b12e91144b315f36c8ebd1d9e0457a352/src/pages/login/LoginForm.tsx)：提交前进行 MD5。
