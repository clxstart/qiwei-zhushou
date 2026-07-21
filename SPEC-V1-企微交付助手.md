# SPEC V1 — 企微交付助手

> 状态：ready-for-agent（本地文档）  
> 来源：grilling 共识（2026-07-20）  
> 代码库：从零新建 monorepo，不复用 `group-contact-bot` / OpenSCRM / LagrangeGo 业务代码  
> 发布说明：当前工作区不是 git 仓库，也未配置 GitHub/GitLab issue tracker；本 spec 落盘于 `docs/`。接入远端 Issues 后可将本文同步为 Issue，并打 `ready-for-agent` 标签。

---

## Problem Statement

交付团队在企业微信客户群里拉群对接后，需要反复做同一套事：说明谁是老板、谁是技术、紧急联系人是谁；发交付注意事项和业务说明；按技术和日期改群名；介绍技术人员；记录定金与总价；发送支付宝收款码；确认尾款结清；并在后台看清每一单收入与结清状态。

现状痛点：

- 全靠人工在群里复制粘贴，易漏、易错、易扯皮。
- 钱和群对不上，对账靠脑力和聊天记录。
- 需要一套**真实可跑**的企微能力：建群能自动喊话，手机上能入账/发码/结清，后台能总账 CRUD 与汇总。
- 不接受第一版就上会话存档付费去「听群聊」；也不要庞大 SCRM 全家桶。

## Solution

从零做一个**清亮精简**的「企微交付助手」：

1. **建群即自动跑流水线**（外部客户群）：角色说明 → 注意事项 → 业务三段文字 → 随机一名技术改群名 → 该技术介绍 → 友好账务卡片。
2. **账记在群上**：无重「项目」模块；拉群人通过**群内账务卡片 + 手机 H5 兜底**录入定金/总价、发尾款码、结清。
3. **系统自动算账与状态**；发码与结清结果回客户群；管理员网页做总账、反结清、补发步骤、花名册与全局配置。
4. **工程形态**：React 编成静态资源，Go 单服务托管静态文件并提供 API；MySQL 持久化；V1 仅自建应用通道（事件 + 发消息 + 改群名 + 卡片回调 + OAuth）。

用户感知的闭环：

```text
外部客户群创建
  → 机器人自动完成交付说明与改名
  → 拉群人在卡片/H5 录入定金与总价（可覆盖）
  → 发支付宝收款码（文字+全局码图）
  → 客户付款（人眼看截图）
  → 拉群人点结清 → 群内通知 + 后台总账更新
```

---

## User Stories

### 建群与自动流水线

1. As a 交付负责人, I want 新建外部客户群后系统自动识别成员角色, so that 群里立刻知道老板、技术、拉群人、紧急联系人是谁.
2. As a 客户（老板）, I want 进群后看到清晰的对接人说明, so that 我知道出了事找谁.
3. As a 技术人员, I want 被点名并收到个人介绍（若我被抽中）, so that 客户了解我的技术栈与工作时间.
4. As a 拉群人, I want 建群后自动发出交付注意事项, so that 条条框框事先说清、减少扯皮.
5. As a 拉群人, I want 建群后自动发出业务流程、特色业务、扩展业务文字, so that 客户了解我们能做什么.
6. As a 拉群人, I want 系统按「日期 + 抽中技术标识 + 固定后缀」自动改群名, so that 群名统一好找.
7. As a 系统, I want 多名技术在群时随机只选定一人并固化到该群, so that 改名与介绍一致且回调重试不换人.
8. As a 系统, I want 群内零技术时写「待分配」并跳过技术向改名与介绍, so that 流水线不崩溃.
9. As a 系统, I want 对内部成员尽量 @、对外部老板只写名字不强行 @, so that 提醒有效且不依赖不可用能力.
10. As a 系统, I want 紧急联系人用全局固定配置（在群则 @，否则姓名+联系方式）, so that 每单不必重填.
11. As a 系统, I want 建群流水线按步骤幂等、失败从断点续跑, so that 回调重试或半失败不会刷屏或卡死.
12. As a 管理员, I want 可对某群补发流水线某一步, so that 失败后能修复而不整单重跑.
13. As a 系统, I want 中途进人时不自动说话, so that 避免重复刷屏.
14. As a 系统, I want 默认只对外部客户群跑流水线, so that 内部群不被打扰.
15. As a 管理员, I want 全局开关允许全部群触发以便自测, so that 开发联调方便.
16. As a 系统, I want 建群流水线末尾自动发友好版账务卡片, so that 拉群人马上有入账/发码/结清入口.
17. As a 客户, I want 账务卡片文案友好、少暴露生硬内部词, so that 观感专业.

### 角色识别与花名册

18. As a 管理员, I want 维护技术花名册（UserID、姓名、技术栈、工作时间、一句话介绍、群名标识、启用）, so that 建群能匹配与介绍.
19. As a 系统, I want 用花名册 UserID 匹配群内技术, so that 不靠猜岗位.
20. As a 系统, I want 将建群者识别为拉群人, so that 权限与角色说明正确.
21. As a 系统, I want 将「非技术且非拉群人」列为老板且多人全列, so that 客户方都出现在说明里.
22. As a 管理员, I want 配置全局紧急联系人, so that 话术统一.
23. As a 管理员, I want 禁用某技术后不再被匹配抽中, so that 离职或暂停接单的人不进自动逻辑.

### 账务（群维度）

24. As a 拉群人, I want 在账务卡片点「录入金额」进入极简 H5 填写定金与总价, so that 手机上也能记账.
25. As a 拉群人, I want 可反复提交覆盖该群定金与总价, so that 改口时不必走复杂改价流程.
26. As a 系统, I want 自动计算尾款 = 总价 - 定金, so that 发码与对账有数.
27. As a 拉群人, I want 点「发尾款码」后群内收到金额文字 + 全局收款码图片, so that 客户能付款.
28. As a 拉群人, I want 未结清前可多次发送尾款码, so that 客户找不到码时可补发.
29. As a 系统, I want 已结清后禁止再发尾款码, so that 降低重复付款风险.
30. As a 系统, I want 从未成功发过尾款码时不允许结清, so that 流程为「先发码 → 客户付 → 结清」.
31. As a 拉群人, I want 确认收到客户款项后点「结清」, so that 该群账务闭环.
32. As a 客户, I want 结清后在群里看到结果通知, so that 双方有据可查.
33. As a 系统, I want 入账成功默认不往群里喊, so that 减少刷屏.
34. As a 拉群人, I want 仅自己（建群者）在卡片/H5 上的操作生效, so that 客户或其他人乱点无效.
35. As a 系统, I want 无权限操作时明确失败提示, so that 操作者知道原因.
36. As a 管理员, I want 在网页后台对任意群做入账/发码/结清兜底, so that 拉群人不可用时不卡单.
37. As a 管理员, I want 反结清已结清群并写日志, so that 误操作或退款场景可纠错.
38. As a 客户, I want 付款后只需在群里发截图给人看, so that 不依赖机器人识图.
39. As a 拉群人, I want 当群内卡片不可用时用 H5 管理链接完成同样三键操作, so that 有兜底不瘫痪.

### 身份与安全

40. As a 拉群人, I want 点卡片时由企微回调携带我的 UserID 完成鉴权, so that 我不用记网站密码.
41. As a 拉群人, I want 打开录入 H5 时经企微免登拿到 UserID, so that 手机一键可用.
42. As an 管理员, I want 用配置文件中的单一账号密码登录后台, so that 运维入口简单可控.
43. As a 系统, I want API 在管理员会话与员工企微身份两条路径上分别鉴权, so that 权限模型清晰.
44. As a 系统, I want 管理链接与 H5 拒绝未授权身份, so that 客户点开不能改账.

### 后台与总账

45. As an 管理员, I want 群列表看到群名、状态、定金、总价、尾款、是否已发码、是否结清, so that 一眼掌控.
46. As an 管理员, I want 群详情查看成员快照、流水线步骤状态、账务与最近日志, so that 排障方便.
47. As an 管理员, I want 总账列表支持每条记录的查看与必要 CRUD, so that 账可维护.
48. As an 管理员, I want 汇总今日/本月收入、未结清数量等, so that 知道钱在哪.
49. As an 管理员, I want 编辑全局配置（注意事项、业务三段、群名后缀默认、紧急联系人、收款码图、触发范围开关等）, so that 不改代码改话术与素材.
50. As an 管理员, I want 上传/更换全局支付宝收款码图片, so that 发码内容正确.
51. As an 管理员, I want 维护技术花名册增删改查, so that 人员变动可更新.
52. As an 管理员, I want 查看操作与发送日志, so that 联调与纠纷可追溯.
53. As an 管理员, I want 在日志中看到谁在何时入账/发码/结清/反结清/补发, so that 责任清晰.

### 工程与交付形态

54. As a 开发者, I want 前端构建为静态文件并由 Go 托管, so that 单服务部署简单.
55. As a 浏览器用户, I want 页面通过 API 取数, so that 前后端职责分离.
56. As a 开发者, I want 代码分层清晰（编排 / 装配 / 企微协议 / 业务 logic / 配置）, so that 后续扩展不糊.
57. As a 开发者, I want 生产环境公网 HTTPS 接收企微回调, so that 建群与卡片点击真实可用.
58. As a 开发者, I want 开发期可用内网穿透联调, so that 本机可测真回调.
59. As a 开发者, I want V1 不引入 Redis 与微服务, so that 依赖最少.
60. As a 运维, I want 敏感配置（CorpID、Secret、Token、AESKey、管理员密码）来自配置/环境而非写死代码, so that 安全可管.

### 对账状态

61. As a 系统, I want 群账务状态在「未入账 / 已入账待发码 / 已发码待结清 / 已结清」间流转, so that 列表可筛选.
62. As a 系统, I want 结清后群侧对拉群人只读（不可入账覆盖生效、不可发码、不可再结清）, so that 状态稳定.
63. As an 管理员, I want 反结清后恢复可发码与可再结清, so that 纠错后流程可继续.

### 文案与模板

64. As an 管理员, I want 角色说明、注意事项、业务三段、技术介绍骨架、发码文案、结清文案可在全局配置中编辑, so that 运营改字不发版.
65. As a 系统, I want 模板使用少量固定变量（如技术名、老板列表、金额、尾款）, so that 实现简单可控.

### 失败与边界

66. As a 系统, I want 企微发消息失败时记录日志并保留步骤未完成状态, so that 可续跑或补发.
67. As a 拉群人, I want 录入非法金额时得到明确校验错误, so that 知道怎么改（实现上建议总价>0、定金≥0、定金≤总价；与「可覆盖」并存）.
68. As a 系统, I want 同一建群事件重复投递不导致重复群发, so that 客户群不被刷.

---

## Implementation Decisions

### 产品与领域决策

1. **从零新建**，不抽取 `group-contact-bot`、OpenSCRM、`LagrangeGo-Template` 业务代码；仅可参考企微能力认知。
2. **先有群，账在群上**；不做重型「项目」实体；**一群一账**。
3. **建群流水线不依赖入账**；入账是后半段。
4. **角色规则**：技术=花名册匹配；拉群人=建群者；老板=其余（多人全列）；紧急联系人=全局固定。
5. **多技术**：匹配列表中 **随机 1 人**，用于角色中的技术位、改群名、介绍；结果持久化到群记录。
6. **@ 策略**：内部能 @ 则 @；外部老板只展示名称。
7. **群名格式**：`{MM-DD}-{技术标识}{固定业务后缀}`（示例风格：`07-20-🔧小程序｜文档`）；后缀全局可配。
8. **业务素材与注意事项**：纯文字；注意事项独立一步；业务为流程/特色/扩展三段。
9. **账务主路径**：卡片 + H5；**不**做会话存档、**不**做客户群自由 @ 命令、**不**做截图识别。
10. **入账可反复覆盖**；**发码**：未结清可多次，已结清不可；**结清**须已发码≥1；结清与发码**回群**，入账默认不回群。
11. **权限**：卡片/H5 仅拉群人；管理员后台可操作任意群并反结清。
12. **触发范围**：默认外部客户群；全局开关可全部群（自测）。
13. **中途进人忽略**。
14. **流水线按步骤幂等** + 管理员补发某步。

### 架构决策

15. **单 Go 进程**：HTTP（API + 企微回调 + 静态资源）+ 业务服务 + MySQL。
16. **分层精神**（对齐用户提供的写法纪律，映射到企微域）：
    - 入口：进程编排、信号退出，不写业务。
    - 装配：依赖注入，创建网关/仓库/用例。
    - 企微协议层：回调校验解密、发消息、改群名、OAuth、卡片回调解析；**无业务规则**。
    - 业务 logic / 应用服务：流水线、记账、权限、状态机。
    - 配置与工具：TOML/YAML、日志。
17. **主应用服务 `DeliveryBot`（名称可调整，职责不变）** 为业务门面，供 HTTP 与回调适配器调用。
18. **`WeComGateway` 接口** 隔离真企微，便于测试与替换。
19. **V1 发送通道仅自建应用**；不做 Webhook 双写。
20. **前端**：薄 React 管理端 + 极简 H5（录入金额/三键）；build 产物由 Go `embed` 或目录托管。
21. **V1 无 Redis**；会话可用 DB/签名 Cookie/JWT 等轻量方案（实现自选，保持简单）。

### 领域状态（决策形状，非最终表名）

群流水线步骤示例：

```text
role_announcement
delivery_notice
biz_flow_text
biz_feature_text
biz_extend_text
rename_group
tech_intro
account_card
```

每步：`pending | done | skipped | failed`；整群续跑时只执行非 `done/skipped`。

群账务状态示例：

```text
unbooked → booked → code_sent → settled
                     ↑              |
                     admin_reverse_settle
```

约束：

- `Settle` 仅当 `code_sent_count >= 1` 且未 settled。
- `SendTailPaymentCode` 在 settled 时拒绝。
- `RecordPayment` 在 settled 时对拉群人拒绝；管理员反结清后方可。

### 数据要点（逻辑模型）

- **TechPerson**：userid, name, stack, work_time, intro, name_emoji/tag, enabled.
- **GroupChat**：chat_id, name, creator_userid, is_external, member_snapshot, selected_tech_userid, pipeline_steps, trigger meta.
- **GroupLedger**：group_chat_id, deposit, total, tail(computed), code_sent_count, status, amounts timestamps.
- **GlobalConfig**：emergency_contact, notice template, three biz texts, rename suffix, payment QR asset, scope switch, message templates.
- **AdminUser**：config-based singleton credentials.
- **OpLog / SendLog**：actor, action, group, payload summary, result, time.

ID：建议雪花或 UUID 字符串；时间 Unix 秒，业务时区 UTC+8。

### API 契约（概念分组，路径实现时可前缀 `/api/v1`）

**企微**

- 回调 URL：URL 验证 + 建群等事件加密回调。
- 卡片点击回调：解析操作者 userid、group、action。
- OAuth/免登：H5 换取 userid。

**管理员（密码会话）**

- 登录/登出。
- 群 CRUD 只读列表与详情；代操作入账/发码/结清/反结清；补发步骤。
- 技术花名册 CRUD。
- 全局配置读写；收款码上传。
- 总账列表与汇总。
- 日志查询。

**拉群人（企微身份）**

- 录入金额（覆盖）。
- 发尾款码。
- 结清。
- 读本群账务摘要（卡片/H5 展示）。

### 交互时序（关键）

**建群**

```text
WeCom → CallbackAdapter → DeliveryBot.OnExternalChatCreated
  → 拉成员、匹配技术、随机固化
  → 逐步 Send* / Rename（幂等标记）
  → SendAccountCard
```

**发码**

```text
Card/H5 → Auth(userid==creator or admin) → DeliveryBot.SendTailPaymentCode
  → WeComGateway.SendText + SendImage
  → code_sent_count++, status=code_sent
```

**结清**

```text
Auth → DeliveryBot.Settle
  → require code_sent_count>=1
  → status=settled, log, Send settle notice to group
```

### 测试 Seams（已与用户对齐）

- **主 seam：`DeliveryBot` + 假 `WeComGateway` + 可注入 Clock/Random + Store**。
- 验收只断言外部行为：发了什么消息/改了什么名/账本与步骤如何变。
- HTTP 与静态托管仅冒烟；不测真企微网络；不测旧仓库。

---

## Testing Decisions

### 什么是好测试

- 只测**外部行为**与业务规则，不测私有函数结构、不测 SQL 文案、不测框架接线细节。
- 给定领域输入（建群事件、actor、金额）→ 断言网关调用序列与持久化后的状态。
- 不依赖真实企微、不依赖本机浏览器 E2E 作为门禁（E2E 可选，非 V1 必须）。

### 优先覆盖模块

1. **DeliveryBot 建群流水线**：外部群触发、内部群默认跳过、开关打开后触发；步骤幂等；随机技术固化；零技术 skipped；@ 策略（内部 vs 外部）在「调用网关的参数」层验证。
2. **DeliveryBot 账务**：覆盖入账；发码次数与结清门槛；已结清拒绝发码；无权限拒绝；管理员反结清后恢复。
3. **权限**：creator vs 其他人 vs admin。
4. **续跑/补发**：中途失败后续跑不重复已 done 步骤。

### Prior art

- 新仓库无历史测试。参考风格：表驱动、接口注入、fake gateway 记录调用。
- 明确**不**沿用 `group-contact-bot` 测试目录结构。

### 建议用例清单（非穷尽）

- 建群：2 技术 → 只介绍抽中者；网关 Rename 使用该技术标识。
- 同一 chat 事件两次 → 第二次无新增 Send。
- 入账 3000/8000 → 尾款 5000；再入账 2000/8000 → 覆盖为定金 2000。
- 未发码结清 → 错误。
- 发码两次 → count=2 仍可结清一次；结清后再发码 → 错误。
- 非拉群人点结清 → 错误；管理员可结清/反结清。

---

## Out of Scope

V1 **明确不做**：

1. 企微会话内容存档及一切「听客户群自由聊天」能力。  
2. 客户群内 `@机器人 定金/结清` 文本命令。  
3. 支付截图识别、见截图自动 @ 建群人。  
4. Webhook 群机器人双通道。  
5. 重型「项目」模块、一项目多群、一群多项目。  
6. 多管理员 RBAC、复杂组织权限。  
7. 中途进人自动欢迎/自动介绍。  
8. 多技术全员详细介绍（仅随机 1 人）。  
9. 复用或改造旧仓代码作为底座。  
10. Redis、消息队列中台、微服务拆分。  
11. 关键词监听催尾款、自动识别聊天意图。  
12. 非官方协议 / 个人号方案。  
13. 完整 SCRM（客户海、侧边栏营销全家桶等）。  
14. 多期自定义账单（仅定金+总价推导尾款）。  
15. 自动根据时间调度催款任务（V1 人点发码即可）。

---

## Further Notes

### 领域词汇表（本 spec 用语）

| 词 | 含义 |
|----|------|
| 拉群人 | 建群者 creator，卡片主操作人 |
| 老板 | 群内非技术且非拉群人的成员（多为客户），可多人 |
| 技术 | 花名册匹配到的内部成员；多人时随机抽 1 |
| 紧急联系人 | 全局固定对接人 |
| 账务卡片 | 建群末尾下发的交互卡片，入账/发码/结清入口 |
| 尾款码 | 全局收款码图 + 当前尾款金额文字 |
| 总账 | 后台按群/流水汇总的收入与结清视图 |
| 流水线 | 建群后有序自动步骤集合 |
| 外部客户群 | 含外部联系人的企微群，默认触发流水线 |

### 与历史文档关系

- `初步规划文档.md` 为早期大而全设想；**以本 SPEC 为准**收敛 V1。  
- 旧目录仅作对照，不作为实现依赖。

### 发布与 tracker

- 本文路径：`docs/SPEC-V1-企微交付助手.md`。  
- 无远端 issue tracker 时，本文即唯一 ready 规格源。  
- 接入 GitHub 后：可开 Issue 粘贴本文，标签 `ready-for-agent`。

### 实现启动条件

- 需企业微信：**CorpID、自建应用 Secret、回调 Token/AESKey、应用权限（客户群事件、发消息、改群名、OAuth、卡片等以实际开通为准）**。  
- 生产：**HTTPS 公网域名**。  
- 开发：穿透 + 上述配置。

### 建议实现顺序（非强制，供 agent）

1. 骨架：配置、日志、MySQL、静态托管、管理员登录。  
2. WeComGateway 真实现 + 回调验签解密。  
3. 花名册与全局配置 API/页。  
4. DeliveryBot 流水线 + 幂等 + 假网关单测。  
5. 账务三动作 + 卡片/H5 + 权限。  
6. 总账与日志。  
7. 真企微联调与补发/反结清。

---

**共识结论**：V1 做「建群自动交付说明 + 群账务闭环 + 后台总账」，官方自建应用能力内真实可跑；不做听群聊与重 SCRM。
