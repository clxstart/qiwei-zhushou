# 企业微信接入方式调研（出站 API、回调与开源实现）

> 调研范围：企业微信自建应用及企业微信群消息推送。结论以企业微信官方文档为准；开源项目仅用于说明常见工程实现。文中不记录任何密钥、Cookie 或服务器密码。

## 先给结论

1. **只调用企微出站 API**：项目服务器向 `qyapi.weixin.qq.com` 发起请求。核心前置条件是启用自建应用、准备企业 ID / 应用 Secret / AgentId，并把服务器公网出口 IP 加入企业微信要求的**企业可信 IP**。不需要可信域名，也不需要接收消息回调。
2. **要接收企微主动推送**：必须另行配置回调服务器 URL、Token、EncodingAESKey，并实现 GET 验证和 POST 验签解密。可信 IP 解决“谁可以调用企微 API”，不是接收回调的地址白名单。
3. **要做网页授权 / 扫码登录**：必须配置应用的可信域名；重定向 URI 的域名必须与配置完全一致，带端口时端口也要一致。企业内部开发域名还要满足企业主体/关联主体的备案要求。
4. **不想备案域名时**：可以做出站 API、同步/查询类能力，以及群机器人 Webhook 发消息；不能依赖网页授权登录，也不能使用需要企业微信主动回调的实时事件链路。

## 1. 官方必需条件与边界

### 1.1 自建应用出站 API

- 服务端先使用企业 `CorpID` 和应用 `Secret` 调用 `GET /cgi-bin/gettoken` 获取 `access_token`；业务 API 再携带该 token 调用。
- `access_token` 必须由后端保存和缓存，不能返回前端；过期或失效后重新获取，不能频繁调用 `gettoken`。
- 具体业务还受应用权限、应用可见范围、接口专属权限和企业类型限制。例如发送应用消息需要 `agentid`，接收人通常必须在应用可见范围内。
- 所有服务端接口使用 HTTPS；官方“开发前必读”说明，新创建的自建应用（以及新开启的通讯录同步助手）需要在管理端配置可信 IP，只有配置的可信 IP 才能调用接口。这里填的是**发起请求的公网出口 IP**，本项目应填写服务器真实公网出口 IP，而不是 URL、路径或回调地址。

来源：

- [获取 access_token](https://developer.work.weixin.qq.com/document/path/91039)
- [开发前必读（可信 IP）](https://developer.work.weixin.qq.com/document/path/90664)
- [发送应用消息](https://developer.work.weixin.qq.com/document/path/90236)

### 1.2 企业可信 IP

- 作用：限制哪些公网出口 IP 可以调用企业微信服务端 API，是**出站调用的安全白名单**。
- 它不等于可信域名，不提供网页访问地址，也不让企微把事件推送到该 IP。
- 服务器若有 NAT、代理、负载均衡或云厂商出口，后台应登记企业微信实际看到的公网出口 IP；本机内网地址没有用。
- 官方文档还提供“获取企业微信接口 IP 段”和“获取企业微信回调 IP 段”接口。前者是企业服务器访问企微 API 时的接口域名 IP 信息；后者是企业微信向回调 URL 发请求时的来源 IP 段，二者用途不要混淆。

来源：

- [开发前必读（可信 IP）](https://developer.work.weixin.qq.com/document/path/90664)
- [获取企业微信接口 IP 段](https://developer.work.weixin.qq.com/document/path/92520)
- [获取企业微信回调 IP 段](https://developer.work.weixin.qq.com/document/path/92521)

### 1.3 可信域名

- 主要用于企业微信终端打开网页时的网页授权、应用主页/菜单/消息卡片中的网页链路，以及其他要求应用可信域名的浏览器能力。
- 网页授权的 `redirect_uri` 域名必须事先配置；配置域名与实际访问域名必须完全一致。若 URL 带端口，端口也要登记；协议和路径不是可信域名匹配的替代项。
- 企业内部开发配置的域名，备案主体必须与企业微信认证/验证主体相同，或属于官方认可的关联主体；否则可能无法配置。官方列举的关联主体包括母子公司、总分公司、同集团企业、同一自然人绝对控股企业等。
- 因此，可信域名不是出站 API 的必需项；但只要做网页授权、扫码登录或需要在企微内打开并完成身份换取，就不能用未满足要求的 IP URL 代替。

来源：

- [网页授权登录：关于网页授权的可信域名](https://developer.work.weixin.qq.com/document/path/91335)
- [构造网页授权链接](https://developer.work.weixin.qq.com/document/path/91022)
- [企业内部开发配置域名指引](https://open.work.weixin.qq.com/wwopen/common/readDocument/40754)

### 1.4 接收消息与事件回调

- 要在企微发生消息、菜单点击、通讯录/客户/客户群等事件时，由企微主动通知项目，需配置回调服务器的三个参数：`URL`、`Token`、`EncodingAESKey`。
- URL 必须由项目提供并能被企业微信访问。官方通用回调配置文档要求服务同时支持 HTTP GET 和 POST：
  - GET：企微携带签名、时间戳、随机数和加密 `echostr` 验证 URL；服务验签、解密后，在 1 秒内返回明文。
  - POST：企微发送加密业务消息；服务验签、解密并在 5 秒内正确响应。官方建议先快速应答，再异步处理业务。
- 回调服务不是“可信 IP”。可信 IP 约束的是项目调用企微 API 的出口；回调 URL 是企微访问项目的入口。防火墙若要限制回调来源，应使用官方“获取企业微信回调 IP 段”接口维护白名单。
- 客户联系场景还需在“客户联系—可调用应用”中配置应用、开启相应的接收事件类型，并确保应用有相应权限；只有完成这些配置，相关事件才会推送。
- 若项目只做出站 API，可以不配置回调；代价是不会实时收到企微事件，需要改用主动查询/轮询或人工触发同步。

来源：

- [回调配置](https://developer.work.weixin.qq.com/document/path/90930)
- [客户联系回调概述](https://developer.work.weixin.qq.com/document/path/92129)
- [客户联系事件格式](https://developer.work.weixin.qq.com/document/path/92130)
- [获取企业微信回调 IP 段](https://developer.work.weixin.qq.com/document/path/92521)

### 1.5 网页授权 / 扫码登录

- 网页授权适用于从企业微信终端打开网页并获得当前成员身份，典型流程是：构造授权链接 → 企微返回一次性 `code` → 后端调用 `GET /cgi-bin/auth/getuserinfo` 换取 `userid` 等身份信息。
- `redirect_uri` 的域名必须先配置为应用可信域名；`code` 只能使用一次并会过期。
- `snsapi_base` 是静默授权、获取基础身份；`snsapi_privateinfo` 是手动授权、获取更多敏感信息，并要求 `agentid` 及成员在应用可见范围内等条件。
- 这条链路依赖浏览器回跳，所以不能用“只配置可信 IP”替代可信域名；服务器 IP URL 也不能绕过域名主体/备案校验。

来源：

- [企业自建应用网页授权开始开发](https://developer.work.weixin.qq.com/document/path/91335)
- [构造网页授权链接](https://developer.work.weixin.qq.com/document/path/91022)
- [获取访问用户身份](https://developer.work.weixin.qq.com/document/path/91023)

## 2. 开源项目常见的三种实现

### 2.1 群机器人 Webhook：最省配置的单向发消息

**做法**：在群里创建消息推送/群机器人，取得该群专属 Webhook；项目服务器直接向 `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...` POST JSON。

**优点**：不需要 `CorpID`、应用 Secret、可信 IP、可信域名或回调服务器；很适合告警、通知、定时播报。

**限制**：Webhook 本质是“向一个已经绑定的群发消息”，不是自建应用的完整 API；它不提供企业通讯录/客户群查询，也不会自动把群成员消息回调给项目。Webhook URL 等同于凭证，必须保密，泄漏后别人可向该群发消息。群聊能力、外部联系人群能力和可用消息类型以当前企微后台及官方文档为准，不能把它与自建应用 API 混称。

官方配置文档明确说明：创建者可在消息推送页面看到该推送专属 Webhook，开发者对该地址发 HTTP POST；同时特别提醒不要公开 Webhook 地址。

来源：

- [消息推送配置说明（原“群机器人”）](https://developer.work.weixin.qq.com/document/path/99110)

成熟开源实现：

- [WxJava：WxCpGroupRobotServiceImpl.java](https://github.com/binarywang/WxJava/blob/develop/weixin-java-cp/src/main/java/me/chanjar/weixin/cp/api/impl/WxCpGroupRobotServiceImpl.java) —— 直接 POST Webhook，使用 `postWithoutToken`，不走 `access_token`。
- [本项目 qiwei-zhushou：Webhook 单元测试](https://github.com/clxstart/qiwei-zhushou/blob/main/internal/wecom/client_webhook_test.go) —— 以 Webhook 作为群消息通道，并验证图片使用 base64 + MD5。

### 2.2 自建应用官方 API + 回调：实时、完整、正式

**做法**：

1. 用自建应用 `Secret` 获取并缓存 `access_token`；
2. 用官方 API 查询通讯录、客户、客户群，发送应用消息、修改允许修改的资源；
3. 配置可访问的回调 URL、Token、EncodingAESKey；
4. 实现 GET 验证、POST 验签解密、快速响应、幂等和异步处理；
5. 网页内需要识别成员时，再额外配置可信域名并接网页授权。

**优点**：能力完整、事件实时、身份和权限边界清晰，适合 SCRM、客户群管理和企业内部流程。

**限制**：后台配置最多，往往需要备案主体匹配的域名和 HTTPS；不同 API 仍有应用可见范围、权限、群类型、频率和企业认证状态限制。尤其要注意：官方“客户群详情/客户群事件”和“应用创建的内部群”不是同一类群，也不代表应用能向所有外部客户群自由发消息。

成熟开源实现：

- [OpenSCRM 回调入口](https://github.com/openscrm/api-server/blob/master/app/callback/callback_handler.go) —— GET 做 URL 验证，POST 读取并解密回调，再按事件类型分发。
- [OpenSCRM 回调加解密入口](https://github.com/openscrm/api-server/blob/master/pkg/easywework/callback.go) —— 验签、解密 `echostr` 和 POST 加密消息。
- [OpenSCRM API 客户端](https://github.com/openscrm/api-server/blob/master/pkg/easywework/client.go) —— 获取并缓存 token，调用客户群、消息等官方服务端 API。
- [MoChat 回调控制器](https://github.com/mochat-cloud/mochat/blob/master/api-server/app/core/corp/src/Action/Dashboard/WeWorkCallback.php) —— 同一路由支持 GET/POST，交给回调组件处理并记录日志。
- [企业微信官方/团队示例库 weworkapi_python](https://github.com/sbzhu/weworkapi_python) —— 同时提供 API 客户端、`VerifyURL`、`DecryptMsg`、`EncryptMsg` 示例；README 还明确建议缓存 token。

### 2.3 官方 API + 轮询：没有回调时的折中方案

**做法**：只配置出站 API 和可信 IP，定时调用列表/详情/结果接口，例如定时拉取客户群列表，再对比本地快照，发现新群或变化后处理。

**优点**：不需要接收消息回调，不需要对外暴露回调 URL；在暂时没有合规域名、或业务只要求分钟级同步时比较实用。

**限制**：有延迟、会消耗接口额度、需要分页、游标、去重、失败重试和基线初始化；API 返回的是“当前状态”，不等于完整事件流水。官方客户群列表接口使用 `cursor + limit` 分页，详情接口还提示群变更后部分信息可能异步更新，因此轮询不能写成无状态的高频全量请求。

成熟开源实现：

- [本项目 GroupSynchronizer](https://github.com/clxstart/qiwei-zhushou/blob/main/internal/delivery/group_synchronizer.go) —— 分页拉取客户群 ID，初始化基线，对新增/待处理群复用业务流水线。
- [本项目 GroupSyncScheduler](https://github.com/clxstart/qiwei-zhushou/blob/main/internal/delivery/group_scheduler.go) —— 以固定间隔执行同步任务，并避免并发运行。
- [MoChat 定时任务示例](https://github.com/mochat-cloud/mochat/blob/master/api-server/plugin/mochat/room-tag-pull/src/Task/RoomTagPull.php) —— 使用定时任务周期拉取/同步群发结果；[同步应用任务](https://github.com/mochat-cloud/mochat/blob/master/api-server/app/core/work-agent/src/Task/SyncWorkAgent.php) 展示定时同步企业微信应用数据。
- [官方客户群列表接口](https://developer.work.weixin.qq.com/document/path/92120) —— 支持游标分页并要求使用有权限的自建应用 token。
- [官方客户群详情接口](https://developer.work.weixin.qq.com/document/path/92122) —— 通过 `chat_id` 查询群名、成员等当前状态。

## 3. 官方合规方案与非官方 Cookie / 逆向方案

### 3.1 官方合规方案

属于官方接口和官方配置范围的做法包括：

- 自建应用 `CorpID + Secret + access_token` 服务端 API；
- 企业可信 IP；
- 应用可信域名与网页授权；
- 回调 URL + Token + EncodingAESKey 的验签/加解密；
- 企业微信群消息推送 Webhook；
- 在接口权限、应用可见范围、群类型、频率限制和企业微信协议允许的范围内使用上述能力。

这类方案的共同点是：调用路径和权限由企业微信官方文档定义，服务端不冒充企微网页客户端，也不依赖个人管理员登录态。

### 3.2 非官方 Cookie / 逆向方案

一些开源工具或商业工具会模拟企业微信管理后台、浏览器或客户端登录，扫码后保存管理后台 Cookie，再调用内部网页接口；也有工具通过 WebView、Hook、协议分析或逆向实现个人号/客户端自动化。它们通常可以做到官方开放 API 没有直接提供的操作，但不属于自建应用官方 API。

可观察到的源码例子：

- [WeComLoad README](https://github.com/Memoyu/WeComLoad) —— 明确写有“获取 Login QrCode”“登录获得 Cookies”，并以 Cookie 进行企微后台操作。
- [WeComLoad HTTP 请求封装](https://github.com/Memoyu/WeComLoad/blob/master/src/WeComLoad.Shared/WeComAdminWebReq.cs) —— 保存 Cookie 字符串并在后续后台请求中带上 Cookie。
- [vbot](https://github.com/Hanson/vbot) 与 [FlowBot](https://github.com/xlrpa/FlowBot) 的项目说明包含 RPA、Hook、协议、逆向等关键词；这些描述本身就表明它们不是单纯的企业微信官方服务端 API SDK。

这类方案的风险和边界：

- Cookie 是管理员/客户端会话凭证，泄漏后可能直接暴露后台权限；不能提交代码仓库、不能写入日志、不能与第三方共享。
- 私有网页接口可能随版本变更、登录策略或风控策略变化而失效，稳定性和可维护性都低于官方 API。
- 它不能把“官方 API 需要的可信 IP/可信域名”变成不需要；只是绕开了部分官方开放 API 配置，合规性、账号安全和服务协议风险需要由使用者自行承担。
- 本调研将其作为识别和风险分类，不把 Cookie、Hook、逆向方法作为本项目实施建议，也不提供绕过企业微信校验的操作步骤。

来源：

- [企业微信软件许可及服务协议](https://work.weixin.qq.com/eula)
- [WeComLoad](https://github.com/Memoyu/WeComLoad)
- [vbot](https://github.com/Hanson/vbot)
- [FlowBot](https://github.com/xlrpa/FlowBot)

## 4. 对当前项目的直接判断

- 当前项目若坚持“只使用企微出站 API”，最低配置是：启用自建应用、准备应用凭证、在企微后台登记服务器真实公网出口 IP、后端缓存 token，并按接口权限调用。
- 当前项目的轮询同步可以替代“客户群变化回调”的实时性，但不能产生企微主动推送事件，也不能替代网页授权身份。
- `http://服务器IP/qiwei/` 可以作为普通浏览器访问的管理页面，但不能据此获得网页授权或接收企微回调；后两者分别需要可信域名或回调服务器配置。
- 如果暂时没有符合主体要求的备案域名，建议先保留出站 API + 轮询，或使用独立的群机器人 Webhook 做单向通知；等域名和 HTTPS 条件满足后，再接官方回调和网页授权。

## 5. 来源索引

官方文档：


- [开发前必读](https://developer.work.weixin.qq.com/document/path/90664)
- [获取 access_token](https://developer.work.weixin.qq.com/document/path/91039)
- [回调配置](https://developer.work.weixin.qq.com/document/path/90930)
- [企业微信接口 IP 段](https://developer.work.weixin.qq.com/document/path/92520)
- [企业微信回调 IP 段](https://developer.work.weixin.qq.com/document/path/92521)
- [发送应用消息](https://developer.work.weixin.qq.com/document/path/90236)
- [应用发送消息到群聊会话](https://developer.work.weixin.qq.com/document/path/90243)
- [应用推送消息](https://developer.work.weixin.qq.com/document/path/90248)
- [消息推送配置说明](https://developer.work.weixin.qq.com/document/path/99110)
- [网页授权开始开发](https://developer.work.weixin.qq.com/document/path/91335)
- [构造网页授权链接](https://developer.work.weixin.qq.com/document/path/91022)
- [获取访问用户身份](https://developer.work.weixin.qq.com/document/path/91023)
- [客户群列表](https://developer.work.weixin.qq.com/document/path/92120)
- [客户群详情](https://developer.work.weixin.qq.com/document/path/92122)
- [企业内部开发配置域名指引](https://open.work.weixin.qq.com/wwopen/common/readDocument/40754)

开源源码：

- [sbzhu/weworkapi_python](https://github.com/sbzhu/weworkapi_python)
- [binarywang/WxJava](https://github.com/binarywang/WxJava)
- [openscrm/api-server](https://github.com/openscrm/api-server)
- [mochat-cloud/mochat](https://github.com/mochat-cloud/mochat)
- [clxstart/qiwei-zhushou](https://github.com/clxstart/qiwei-zhushou)
- [Memoyu/WeComLoad](https://github.com/Memoyu/WeComLoad)

