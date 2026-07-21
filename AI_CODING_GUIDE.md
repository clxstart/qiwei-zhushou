# AI Coding Guide — LagrangeGo-Template 写法规范

> 给 AI / 开发者的长期依赖文档。  
> 目标：后续新增功能时，**严格按本仓库既有写法扩展**，不要另起一套风格。

---

## 0. 一句话总原则

```
组合 > 继承
接口 + 多实现 > 巨型 switch
DI 容器装配 > 到处 new
路由/匹配器/中间件 > 在事件回调里堆 if
业务只写在 logic，协议/登录只写在 bot
入口 main 只做编排，不写业务
```

写代码前先问自己：

1. 这是配置 / Bot 协议 / 业务逻辑 / 工具，该放哪一层？
2. 能不能用现有 `Matcher` / `Middleware` / `Handler` / `Strategy` 扩展？
3. 是否破坏了「组合、接口、分层」？

能扩展现有接口就扩展，不要复制粘贴一套平行体系。

---

## 1. 分层与职责（强制）

| 目录 | 职责 | 允许做 | 禁止做 |
|------|------|--------|--------|
| `main.go` | 启动编排、信号退出 | 调 Container、Login、注册逻辑 | 写消息业务、写登录细节 |
| `app/` | 依赖注入装配 | 创建/串联组件、提供 Getter | 写业务规则 |
| `bot/` | 登录、认证、连接、协议客户端包装 | Strategy、连接状态、sig 管理 | 处理具体聊天业务 |
| `config/` | 配置结构与加载 | TOML 映射、全局配置 | 业务判断 |
| `logic/` | 消息路由、匹配、中间件、事件、业务 Handler | 注册命令/消息处理 | 直接管理登录/重连 |
| `utils/` | 日志等通用能力 | 无业务通用工具 | 依赖 bot/logic 业务类型 |

依赖方向（只能向下，不能反向乱引）：

```
main → app → bot / logic / config / utils
logic → utils, LagrangeGo client/message/event
bot   → utils, LagrangeGo client/auth
config → 仅配置库
utils → 日志等基础库
```

---

## 2. 启动与装配写法

### 2.1 main 只编排

```go
func main() {
    container := app.NewContainer()
    if err := container.Initialize(); err != nil {
        panic(err)
    }

    bot := container.GetBot()
    logicManager := container.GetLogicManager()

    if err := bot.Login(); err != nil {
        panic(err)
    }
    bot.Listen()

    logic.Manager = logicManager
    logic.RegisterCustomLogic()
    logicManager.SetupEventListeners()

    defer bot.Client().Release()
    defer bot.Dumpsig()

    // signal 阻塞退出
}
```

### 2.2 新依赖放进 Container

新增跨层共享组件时：

1. 在 `app.Container` 加字段
2. 在 `Initialize()` 里按依赖顺序创建
3. 提供 `GetXxx()` 
4. **不要**在业务包里偷偷全局 `new` 重要组件（`logic.Manager` 这类既有全局入口除外，新增优先走 Container）

```go
// 好：在 Container 中装配
c.foo = NewFoo(c.client)

// 差：在 handlers 深处随意 New 并持有全局状态
```

---

## 3. Bot 层写法

### 3.1 组合，不继承

```go
// 正确：组合
type Bot struct {
    client        *client.QQClient
    loginMgr      *LoginManager
    authMgr       *AuthManager
    connectionMgr *ConnectionManager
}

// 错误：去“继承”或把所有逻辑塞进一个大 Bot
```

每个 Manager 单一职责：

- `LoginManager`：登录策略编排
- `AuthManager`：sig 读写
- `ConnectionManager`：连接状态 / 重连 / 心跳
- `QRCodeProcessor`：二维码展示

### 3.2 策略模式扩展登录

新增登录方式 = 实现接口 + 注册，不要改死 `if/else` 主流程。

```go
type LoginStrategy interface {
    Login(ctx context.Context, client *client.QQClient) error
    GetStrategyName() string
}

type PasswordLoginStrategy struct {
    logger utils.Logger
}

func (s *PasswordLoginStrategy) GetStrategyName() string { return "密码登录" }

func (s *PasswordLoginStrategy) Login(ctx context.Context, client *client.QQClient) error {
    // 实现...
    return nil
}

// 注册
lm.RegisterStrategy(&PasswordLoginStrategy{logger: lm.logger})
```

登录相关必须考虑：

- `context.WithTimeout`
- 有限次重试
- 明确错误返回，不吞错

### 3.3 连接事件用接口观察者

```go
type ConnectionEventHandler interface {
    OnConnected(client *client.QQClient)
    OnDisconnected(client *client.QQClient, reason string)
    OnReconnecting(client *client.QQClient, attempt int)
    OnReconnectFailed(client *client.QQClient, maxAttempts int)
}
```

并发写法约定：

- 状态：`sync.RWMutex`
- 后台任务：`WaitGroup` + `stopChan chan struct{}`
- 停止：`close(stopChan)` 后 `Wait()`

---

## 4. Logic 层写法（业务主战场）

### 4.1 业务扩展入口

**默认只改 / 只新增：**

1. `logic/handlers.go` → `RegisterCustomLogic()`
2. 必要时新增 `logic/xxx_handler.go`、`matcher`、`middleware`
3. 不要把业务写进 `bot/` 或 `main.go`

```go
func RegisterCustomLogic() {
    if Manager == nil {
        utils.Error("LogicManager 未初始化")
        return
    }

    // 命令
    Manager.HandleCommand("/", "ping", func(ctx *MessageContext) error {
        // ...
        return nil
    })

    // 群消息 + 匹配器
    Manager.HandleGroupMessage(func(ctx *MessageContext) error {
        // ...
        return nil
    }, NewTextMatcher("hello", false))

    // 事件
    Manager.GetEventBus().Subscribe(EventTypeCommandExecuted, func(ctx context.Context, event Event) error {
        return nil
    })
}
```

### 4.2 消息上下文 MessageContext

所有 Handler 只依赖 `*MessageContext`，不要直接吃裸事件类型作为第一选择。

```go
text := ctx.GetMessageText()

if privateMsg, ok := ctx.GetPrivateMessage(); ok {
    ctx.Client.SendPrivateMessage(privateMsg.Sender.Uin, []message.IMessageElement{
        message.NewText("reply"),
    })
}

if groupMsg, ok := ctx.GetGroupMessage(); ok {
    ctx.Client.SendGroupMessage(groupMsg.GroupUin, []message.IMessageElement{
        message.NewText("reply"),
    })
}

// 元数据
ctx.Set("command", "ping")
cmd := ctx.GetString("command")
```

约定：

- 用类型断言 helper，不到处手写 `msg.(*message.GroupMessage)`
- 跨中间件/handler 的临时数据放 `Metadata`（`Set/Get`）

### 4.3 Handler 的两种写法

**A. 函数式（简单逻辑优先）**

```go
Manager.HandleCommand("/", "status", func(ctx *MessageContext) error {
    // ...
    return nil
})
```

**B. 结构体实现接口（可复用 / 有状态 / 较复杂）**

```go
type StatusHandler struct{}

func (h *StatusHandler) Handle(ctx *MessageContext) error {
    // ...
    return nil
}

// 注册
Manager.HandleCommand("/", "status", func(ctx *MessageContext) error {
    return (&StatusHandler{}).Handle(ctx)
})
```

接口约定：

```go
type Handler interface {
    Handle(ctx *MessageContext) error
}

type HandlerFunc func(ctx *MessageContext) error
```

错误处理约定：

- 可恢复/可忽略：打日志后 `return nil` 或返回 error 交给路由 errorHandler
- 不要在 handler 里 `panic`（有 RecoveryMiddleware，但不应依赖它当正常控制流）

### 4.4 Matcher：用组合，不要巨石 if

```go
type Matcher interface {
    Match(ctx *MessageContext) bool
}
```

优先使用现成匹配器：

| 需求 | 使用 |
|------|------|
| 仅群/私聊/好友请求 | `NewMessageTypeMatcher("group"/"private"/"friend_request")` |
| 文本包含 | `NewTextMatcher("hello", false)` |
| 正则 | `NewRegexMatcher(`^\d+$`)` |
| 前缀 | `NewPrefixMatcher("/")` |
| 指定用户 | `NewSenderMatcher(123, 456)` |
| 指定群 | `NewGroupMatcher(111)` |
| 命令 | `NewCommandMatcher("/", "ping")` |
| 与/或/非 | `NewAndMatcher` / `NewOrMatcher` / `NewNotMatcher` |
| @机器人 | `NewAtMatcher(botUin)` |
| 特殊规则 | `NewCustomMatcher(func(ctx *MessageContext) bool { ... })` |

新增匹配器模板：

```go
type XxxMatcher struct {
    // fields
}

func (m *XxxMatcher) Match(ctx *MessageContext) bool {
    // pure decision, 可写 ctx.Set 附加信息，但尽量别有副作用
    return true
}

func NewXxxMatcher(/* args */) *XxxMatcher {
    return &XxxMatcher{ /* ... */ }
}
```

组合示例：

```go
// 群里、指定群、且是 /admin 命令
Manager.HandleGroupMessage(handler,
    NewAndMatcher(
        NewGroupMatcher(123456),
        NewCommandMatcher("/", "admin"),
    ),
)
```

### 4.5 Middleware：洋葱模型

```go
type Middleware func(HandlerFunc) HandlerFunc
```

写法模板：

```go
func XxxMiddleware(/* 配置 */) Middleware {
    return func(next HandlerFunc) HandlerFunc {
        return func(ctx *MessageContext) error {
            // before
            err := next(ctx)
            // after
            return err
        }
    }
}
```

使用位置：

```go
// 全局
Manager.UseMiddleware(RecoveryMiddleware())
Manager.UseMiddleware(LoggingMiddleware())

// 单路由
Manager.HandleCommand("/", "admin", handler,
    AuthMiddleware([]uint32{12345}),
    RateLimitMiddleware(5, time.Minute),
)
```

内置优先复用：

- `LoggingMiddleware`
- `RecoveryMiddleware`
- `RateLimitMiddleware`
- `AuthMiddleware`
- `GroupOnlyMiddleware` / `PrivateOnlyMiddleware`
- `MetricsMiddleware`
- `ConditionalMiddleware` / `ChainMiddleware`

注意：

- 中间件只做横切：日志、鉴权、限流、recover、指标
- **不要**在中间件里写具体业务回复（除非是统一拒绝文案）

### 4.6 Router / LogicManager 扩展方式

优先用 LogicManager 便捷方法：

```go
HandlePrivateMessage(handler, matchers...)
HandleGroupMessage(handler, matchers...)
HandleFriendRequest(handler, matchers...)
HandleCommand(prefix, command, handler, middlewares...)
```

需要完全自定义时：

```go
route := NewRoute("my_route", NewHandlerAdapter(handler))
route.Match(NewMessageTypeMatcher("group"))
route.Match(NewPrefixMatcher("!"))
route.Use(LoggingMiddleware())
Manager.AddRoute(route)
```

事件监听挂载只放在 `SetupEventListeners()` 或其明确扩展点，不要分散订阅导致重复处理。

### 4.7 EventBus 写法

```go
// 订阅
Manager.GetEventBus().Subscribe(EventTypeCommandExecuted, func(ctx context.Context, event Event) error {
    // 异步场景注意超时与幂等
    return nil
})

// 发布（logic 内部已有便捷函数优先用）
PublishMessageReceived(ctx)
PublishMessageProcessed(ctx)
PublishCommandExecuted(ctx, "ping")
PublishError(err, ctx)
```

新增事件：

1. 在 `eventbus.go` 加常量：`EventTypeXxx = "xxx.yyy"`
2. 如需强类型，定义 `XxxEvent` 嵌入 `*BaseEvent`
3. 提供 `PublishXxx(...)` 便捷函数
4. 在业务点 `Publish`，在监听点 `Subscribe`

约定：

- 默认异步 `Publish`；需要严格顺序/错误返回用 `PublishSync`
- handler 必须处理 error / panic（总线已有 recover，但仍应稳健）

---

## 5. 配置写法

```go
// config/config.go
type Config struct {
    Bot BotConfig
    // 新增配置段：
    // Feature FeatureConfig
}

type FeatureConfig struct {
    Enable bool   `toml:"enable"`
    Token  string `toml:"token"`
}
```

```toml
# application.toml
[bot]
account = 114514
password = "pwd"
signServer = "https://..."

# [feature]
# enable = true
# token = "xxx"
```

约定：

- 字段名导出 + `toml` tag
- 全局读取：`config.GlobalConfig`
- 不要把密钥写进代码；只放配置文件（且注意勿提交真实密钥）

---

## 6. 日志写法

统一用 `utils`，不要直接到处 `fmt.Println`。

```go
utils.Info("自定义逻辑注册完成")
utils.Infof("用户 %d 发送了消息", userID)
utils.Warnf("读取签名文件失败: %v", err)
utils.Errorf("处理失败: %v", err)

utils.WithField("module", "auth").Info("签名文件加载成功")
utils.WithFields(map[string]interface{}{
    "user_id": 12345,
    "action":  "login",
}).Info("用户登录")
```

模块内可缓存带字段 logger：

```go
logger: utils.GetLogger().WithField("module", "login")
```

级别约定：

- `Debug`：开发细节、心跳
- `Info`：关键生命周期、注册完成、连接成功
- `Warn`：可降级失败（读 sig 失败、某策略登录失败）
- `Error`：真实故障
- 不要用 `Fatal/Panic` 处理普通业务错误（启动期配置失败除外）

---

## 7. 命名与代码风格

### 命名

| 类型 | 风格 | 示例 |
|------|------|------|
| 接口 | 名词 / 能力 | `Matcher`, `LoginStrategy`, `Handler` |
| 实现 | 具体名 + 接口名 | `TextMatcher`, `FastLoginStrategy` |
| 构造函数 | `NewXxx` | `NewBot`, `NewTextMatcher` |
| 默认配置 | `DefaultXxx` | `DefaultLoginContext` |
| 中间件 | `XxxMiddleware` | `AuthMiddleware()` |
| 注册入口 | `RegisterXxx` | `RegisterCustomLogic` |
| Getter | `GetXxx` | `GetLogicManager` |
| 包级全局 | 导出大写或明确注释 | `Manager`, `GlobalConfig`, `GlobalEventBus` |

### 风格

- 小而专的 struct / 文件，避免 1000 行杂糅
- 导出方法写清职责；未导出 helper 放同包
- 错误：`fmt.Errorf` / `errors.New`，上层日志 + 返回
- 锁粒度尽量小；复制 slice 再遍历，避免持锁执行业务
- 协程必须可停止或有明确生命周期（参考 ConnectionManager / EventBus）
- 保持现有注释密度：包/导出类型有简短中文注释即可，不写废话

### 中文注释约定（与本仓库一致）

```go
// LogicManager 新的逻辑管理器
// HandleCommand 处理命令的便捷方法
// LoadSig 加载签名文件
```

---

## 8. 标准扩展 Checklist

### A. 新增一个命令 `/weather`

1. 在 `RegisterCustomLogic`：
   ```go
   Manager.HandleCommand("/", "weather", func(ctx *MessageContext) error {
       args, _ := ctx.Get("args")
       // 解析 args，调用服务，回复
       return nil
   }, RateLimitMiddleware(3, time.Minute))
   ```
2. 需要权限则加 `AuthMiddleware([]uint32{...})`
3. 成功后如需统计：`PublishCommandExecuted(ctx, "weather")`
4. **不改** bot/main，除非要新配置

### B. 新增关键词自动回复

```go
Manager.HandleGroupMessage(func(ctx *MessageContext) error {
    if groupMsg, ok := ctx.GetGroupMessage(); ok {
        _, _ = ctx.Client.SendGroupMessage(groupMsg.GroupUin, []message.IMessageElement{
            message.NewText("收到关键词"),
        })
    }
    return nil
}, NewTextMatcher("签到", false))
```

### C. 新增匹配器

1. `logic/matcher.go` 增加类型 + `NewXxxMatcher`
2. 实现 `Match(*MessageContext) bool`
3. 在注册路由时组合使用

### D. 新增中间件

1. `logic/middleware.go` 增加 `XxxMiddleware()`
2. 返回 `func(next HandlerFunc) HandlerFunc`
3. 全局 `UseMiddleware` 或路由级传入

### E. 新增登录策略

1. `bot/login.go` 实现 `LoginStrategy`
2. `LoginManager.RegisterStrategy(...)`
3. 保持超时与重试语义

### F. 新增配置项

1. 扩展 `config.Config` 字段 + toml tag
2. 更新 `application.toml` 示例
3. 从 `config.GlobalConfig` 读取
4. 在 Container 初始化阶段注入需要的组件

### G. 新增事件

1. 常量 +（可选）事件结构
2. `PublishXxx`
3. `Subscribe` 处理

---

## 9. 推荐代码模板（直接套）

### 9.1 完整 Handler 文件模板

```go
package logic

import (
    "github.com/ExquisiteCore/LagrangeGo-Template/utils"
    "github.com/LagrangeDev/LagrangeGo/message"
)

// DemoHandler 示例处理器
type DemoHandler struct{}

func (h *DemoHandler) Handle(ctx *MessageContext) error {
    text := ctx.GetMessageText()
    utils.Infof("DemoHandler 收到: %s", text)

    reply := message.NewText("ok")
    if privateMsg, ok := ctx.GetPrivateMessage(); ok {
        ctx.Client.SendPrivateMessage(privateMsg.Sender.Uin, []message.IMessageElement{reply})
        return nil
    }
    if groupMsg, ok := ctx.GetGroupMessage(); ok {
        ctx.Client.SendGroupMessage(groupMsg.GroupUin, []message.IMessageElement{reply})
        return nil
    }
    return nil
}

func registerDemo() {
    Manager.HandleCommand("/", "demo", func(ctx *MessageContext) error {
        return (&DemoHandler{}).Handle(ctx)
    })
}
```

### 9.2 自定义 Matcher 模板

```go
type MinLengthMatcher struct {
    Min int
}

func (m *MinLengthMatcher) Match(ctx *MessageContext) bool {
    return len([]rune(ctx.GetMessageText())) >= m.Min
}

func NewMinLengthMatcher(min int) *MinLengthMatcher {
    return &MinLengthMatcher{Min: min}
}
```

### 9.3 自定义 Middleware 模板

```go
func DenyKeywordMiddleware(keyword string) Middleware {
    return func(next HandlerFunc) HandlerFunc {
        return func(ctx *MessageContext) error {
            if strings.Contains(ctx.GetMessageText(), keyword) {
                return fmt.Errorf("消息包含禁止关键词")
            }
            return next(ctx)
        }
    }
}
```

---

## 10. 反模式（AI 禁止输出这类代码）

1. **在 `main.go` 写群消息 if/else 业务**
2. **继承式巨型 Bot**，把登录/路由/业务揉一起
3. **不用 Matcher，在所有 handler 开头手写重复过滤**
4. **不用 Middleware，在每个命令复制鉴权/限流代码**
5. **业务里直接 `os.Exit` / 随便 `panic`**
6. **新开一套全局事件系统，绕过现有 EventBus**
7. **在 logic 里管理二维码登录/重连**
8. **用 `fmt.Println` 替代 utils 日志**
9. **无锁并发写 map**（限流中间件若扩展需注意线程安全）
10. **为了快而破坏分层**：例如 config 导入 logic

---

## 11. AI 执行任务时的默认工作流

收到新需求时按此顺序：

1. **归类**：配置 / 登录连接 / 消息业务 / 日志工具 / 事件
2. **找扩展点**：Handler / Matcher / Middleware / Strategy / Config / Event
3. **优先最小改动**：能只改 `RegisterCustomLogic` 就不要大重构
4. **保持风格**：`NewXxx`、接口、中文简注、utils 日志、error 返回
5. **补注册**：写了 Handler 必须挂到 Manager；写了 Strategy 必须 Register
6. **不扩 scope**：不顺手重构无关文件，除非阻碍实现
7. **验证思路**：说明消息从 Subscribe → MessageContext → Match → Middleware → Handle 的路径是否打通

---

## 12. 架构速记伪代码

```text
application.toml
    -> config.Init()
        -> app.Container.Initialize()
            -> QQClient + Bot(Login/Auth/Connection) + LogicManager(Router+EventBus)

Bot.Login()
    -> LoginStrategy 依次尝试 (FastLogin / QRCode / ...)

Bot.Listen()
    -> ConnectionManager 监控断开/重连/心跳

LogicManager.SetupEventListeners()
    -> Private/Group/FriendRequest Subscribe
        -> NewMessageContext
            -> PublishMessageReceived
            -> Router: Matchers 全过?
                -> 全局 Middleware + 路由 Middleware 包装
                -> Handler.Handle
            -> PublishMessageProcessed

RegisterCustomLogic()
    -> 只在这里挂业务命令/关键词/订阅
```

---

## 13. 给 AI 的最终约束（可复制到系统提示）

```text
你正在维护 LagrangeGo-Template 风格的 Go 项目。
必须遵守：
1. 组合优于继承；接口多实现扩展。
2. 业务只加在 logic（尤其 RegisterCustomLogic），登录连接只在 bot，装配在 app，配置在 config。
3. 消息处理统一 MessageContext + HandlerFunc/Handler + Matcher + Middleware + Router。
4. 新增能力优先实现现有接口（Matcher/Middleware/LoginStrategy/Event），禁止另起平行框架。
5. 日志用 utils；错误返回 error；并发用 Mutex/WaitGroup/stopChan/context。
6. 命名 NewXxx / XxxMiddleware / HandleCommand 等与仓库一致；注释简短中文。
7. 最小改动完成需求，不无关重构，不把业务塞进 main。
```

---

## 14. 版本与库基线（写代码时对齐）

- Go：`1.23+`（见 `go.mod`）
- 核心协议：`github.com/LagrangeDev/LagrangeGo`
- 配置：`github.com/BurntSushi/toml`
- 日志：`github.com/sirupsen/logrus`（经 `utils` 封装）

引入第三方库前先判断：是否已有封装可复用；是否真正属于该层职责。

---

**文档用途**：后续让 AI「按这个仓库的写法继续写」。  
**业务新增默认落点**：`logic/handlers.go` 的 `RegisterCustomLogic()`。  
**架构新增默认落点**：对应接口实现 + Container/Manager 注册。
