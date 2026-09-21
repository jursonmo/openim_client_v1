手机号和密码登录时，`openim-chat` 会一次性返回这两个 token。这两个 token 分别用于 **两个不同的服务，不能混用**：

| Token | 用于哪个服务 | 什么时候使用 |
|---|---|---|
| `imToken` | `open-im-server`，即时通信服务 | 登录 IM SDK、建立消息连接、获取好友列表、收发消息 |
| `chatToken` | `openim-chat`，业务账号服务 | 调用需要登录身份的业务接口，如修改密码、修改个人资料、搜索用户 |


**`imToken`：交给 SDK 登录**

当前示例中已经这样使用：

```go
open_im_sdk.Login(callback, operationID, result.UserID, result.IMToken)
```

登录之后，SDK 会管理这个凭证。后续调用好友列表、发送消息等方法时，不需要你每次手动传 token。

**`chatToken`：调用业务 HTTP 接口时放进请求头**

例如调用 `openim-chat` 的 `/user/update` 或 `/account/password/change`：

```go
req.Header.Set("token", result.ChatToken)
```

官方业务服务从名为 `token` 的请求头读取并校验凭证，使用方式见[官方鉴权源码](https://github.com/openimsdk/chat/blob/73e7b82a64fdc997a415f3a55f697e0c5800f1d1/internal/api/mw/mw.go)。

**当前程序只涉及好友列表和聊天，所以实际使用的是 `imToken`；`chatToken` 目前仅打印，等后续增加账号资料、密码等业务功能时再使用。**