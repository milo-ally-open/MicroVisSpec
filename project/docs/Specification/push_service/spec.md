# push-service 实现任务书

> 状态：首发实现任务书；以[产品需求](../../Analysis/PRD.md)、[架构设计](../../Design/STRUCTURE.md)和[总技术规格](../../Analysis/SPEC.md)为准。
>
> 责任边界：push-service 管理浏览器 Push 订阅及通知投递；event-service 拥有事件并可靠发出 `event.created`；auth-service 拥有会话并发出 `session.revoked`；Vue PWA 负责权限交互和 service worker 展示。
>
> 首发边界：向当前账号已授权的手机 PWA 发送事件摘要并打开详情。原生 Android、iOS、Harmony 推送和“用户已读”回执不在首发范围。

## 稳定需求与验收索引

> 本索引用于跨文档追踪；详细行为以本文件正文为准。编号保持稳定，需求变更时同步更新。

### REQ-01 订阅身份与租户

订阅绑定经验证账号和会话。

- **AC-01.1**：订阅操作校验身份并安全存储密钥。
- **AC-01.2**：账号切换或 endpoint 复用不串发旧账号通知。

### REQ-02 幂等与撤销

重复上游事件不重复建任务，撤销能抑制投递。

- **AC-02.1**：重复事件不产生重复 inbox/delivery。
- **AC-02.2**：会话撤销停用相关订阅和待投递任务。

### REQ-03 投递结果

投递按临时/永久失败分类，结果语义准确。

- **AC-03.1**：临时失败重试，永久无效订阅停止并清理。
- **AC-03.2**：发送被接受不等价于用户收到或已读。

### REQ-04 PWA 与隐私

用户主动授权后可收到通知并安全打开详情。

- **AC-04.1**：点击进入受鉴权事件详情。
- **AC-04.2**：载荷和 service worker 缓存不暴露敏感信息。

## 一、任务目标

交付独立运行的 Go/Gin/GORM Push 服务及相应 PWA 接线，使用户主动订阅后，即使页面未停留在告警列表，也能通过系统通知得知本账号异常事件。通知只携带必要摘要，点击后仍须由业务接口验证账号与登录状态。

阶段顺序：服务基础 → 浏览器订阅 → 可靠事件接入 → Web Push 投递 → PWA 整链路验收。

---

## 二、第一阶段：服务基础与订阅数据

### 2.1 实现任务

1. 在 `project/src/services/push-service` 中建立 Go 模块、Gin 入口、配置和迁移；发布时构建为独立 Go 二进制，由 systemd 管理。提供内部 `/health/live`、`/health/ready`，就绪状态反映 PostgreSQL 和投递任务所需配置。
2. 在独占的 PostgreSQL `push` schema 中建立 `subscriptions`、`inbox`、`deliveries`。`endpoint` 全局唯一；inbox 以 `(producer,event_id,event_type)` 去重；delivery 以 `(event_id,subscription_id)` 唯一。
3. 订阅保存 `account_id`、创建时 `source_session_id`、endpoint、加密后的 `p256dh`/`auth` 密钥、启用/停用时间；投递保存尝试次数、下次重试时间、响应类别与最终状态。数据库账号不得访问 event/auth 的 schema。
4. 生成或导入 VAPID 密钥对；私钥与订阅密钥的加密主密钥由部署机密提供，不写入仓库、镜像和日志。日志也不记录完整 Web Push endpoint。

### 2.2 交付物与阶段验收

- 交付服务二进制、迁移、健康接口、VAPID 公钥配置和机密轮换说明。
- 空库可迁移；服务重启后订阅与待投递任务仍存在；VAPID 私钥不会经公开 API 返回。
- 用受限数据库账号不能读写其他服务 schema；数据库/关键配置缺失时就绪状态如实显示。

---

## 三、第二阶段：订阅、解绑和账号归属

### 3.1 浏览器 API

| 方法与路径 | 任务与返回 |
| --- | --- |
| `GET /api/v1/push/vapid-public-key` | 返回用于 `PushManager.subscribe` 的 VAPID 公钥。 |
| `PUT /api/v1/push/subscriptions` | 接收 `endpoint`、`keys.p256dh`、`keys.auth`，绑定当前可信账号与会话，返回 `subscription_id`。 |
| `GET /api/v1/push/subscriptions` | 查询当前账号/浏览器的订阅状态，不返回密钥材料。 |
| `DELETE /api/v1/push/subscriptions/{subscriptionId}` | 停用本账号订阅；重复停用视为成功。 |

### 3.2 实现任务

1. 订阅与解绑 API 仅接受已验证浏览器会话，遵守同源 `Origin` 与 `X-CSRF-Token` 校验；`account_id`、`session_id` 只取可信身份上下文，不接受客户端自报归属。
2. 服务端校验 endpoint 协议和长度、密钥格式以及请求大小。相同 endpoint 再次登记应更新为当前账号和会话绑定；若原来属于另一账号，原绑定和未发送任务必须失效，不能向新账号浏览器泄露旧账号事件。
3. 每次成功登录、恢复会话及进入通知设置页时，比较浏览器当前 `PushSubscription` 与服务端状态；已有订阅要重新绑定当前 `session_id`，无需再次请求权限。浏览器不再持有订阅时清理过时服务端绑定；同步失败时 UI 不显示“已正常启用”。订阅 API 不替用户触发浏览器权限弹窗。
4. 前端退出登录前主动解绑当前浏览器订阅；`session.revoked` 负责在前端未能解绑时从服务端补偿。普通会话自然过期不取消用户已同意的后台订阅；通知详情页仍需新会话登录。
5. 审计订阅创建、账号切换与解绑；API 错误不暴露订阅密钥、完整 endpoint 或其他账号是否持有该 endpoint。

### 3.3 交付物与阶段验收

- 交付四条 API、输入/错误合同、当前账号通知设置接线。
- 未授权浏览器无法登记或删除订阅；账号 A 无法查看或解绑账号 B 的订阅。
- 同一 endpoint 重复登记不创建并行有效订阅；浏览器切换账号后旧账号通知不能再送到该 endpoint。

---

## 四、第三阶段：可靠接入事件与会话撤销

### 4.1 服务间接口

push-service 在内网 `/internal/v1/inbox` 接收经服务凭据认证的 `event.created` 与 `session.revoked`。请求须包含生产者、`event_type`、稳定 `event_id`、`schema_version`、`occurred_at` 和负载；具体消息合同遵照[总技术规格 §4.2](../../Analysis/SPEC.md)。

### 4.2 实现任务

1. 校验生产者身份、消息版本和必需字段，在同一数据库事务中写 inbox 和待投递记录。重复 `(producer,event_id,event_type)` 返回成功，但不重复创建投递任务。
2. `event.created` 负载仅接受由 event-service 提供的 `account_id`、事件 ID、地点、危险等级、发生时间和站内详情路径；只为该账号当前有效订阅创建 delivery。首发仅对 device-service 校验通过的已启用课堂异常规则事件创建通知；不得直接读取 event-service 数据库或相信来自浏览器的事件归属。
3. `session.revoked` 根据 `account_id` 与 `session_id` 停用来源会话对应订阅，并取消尚未发出的投递。重复撤销幂等；会话自然到期不由本服务自动解释为撤销事件。
4. 为乱序到达的撤销/新订阅和并发投递定义数据库事务顺序：发送前重新检查订阅是否仍有效，停用后不再领取新的任务；账号切换需同时处理旧绑定与待发送任务。
5. inbox 和 delivery 记录保留必要状态，便于生产者重投、服务重启恢复和查明失败原因；内部接口只在服务网络开放。

### 4.3 交付物与阶段验收

- 交付内部消息 schema、inbox 幂等约束、投递任务状态机和撤销处理说明。
- event-service 重复投递同一消息只产生一组任务；推送服务暂时停机后恢复能处理保留在生产者 outbox 的事件。
- 显式退出或改密撤销订阅后不再领取对应通知；自然到期的会话所关联订阅仍可用于后台通知。

---

## 五、第四阶段：Web Push 投递和故障处理

### 5.1 实现任务

1. 使用 VAPID 签名按 Web Push 协议向浏览器 Push endpoint 发送数据；每个投递任务只含事件地点、危险等级、发生时间、`event_id` 和受限的 `/events/{eventId}` 站内路径，不附截图、账号密钥或 RTSP 信息。
2. worker 使用数据库锁或等价租约领取任务，崩溃后可重新领取；发送前再次核对订阅启用状态、所属账号和任务有效期。HTTP 成功只记为推送服务 `accepted`，不得记为设备显示成功、用户收到或已读。
3. 404/410 等永久失效响应停用订阅；429/5xx 和网络错误按退避计划重试。事件过旧时停止重试并记录原因；失败记录可查询和告警，不静默丢弃。
4. delivery 唯一约束限制平台内部重复任务；Web Push 和网络重试仍可能让客户端收到重复通知，service worker 可用 `event_id` 作为通知 tag 合并显示。事件列表始终是可追溯的业务来源。
5. 记录队列积压、accepted、临时失败、永久失效与过期停止数；日志用 event_id 和 request_id 串联，隐藏 endpoint 与密钥。

### 5.2 交付物与阶段验收

- 交付投递器、VAPID 配置、错误分类与重试规则、可观测指标。
- 模拟订阅失效后该订阅被停用；模拟暂时失败后任务保留并重试，重启服务不丢待发任务。
- 相同事件重复送达内部 inbox 不造成重复平台任务；记录清楚区分“推送服务接受”和“用户看到通知”。

---

## 六、第五阶段：Vue PWA 接线和最终验收

### 6.1 前端实现任务

1. 在现有 Vue 3 + TypeScript Web 工程中接入通知设置页。仅在用户主动操作、浏览器支持且处于安全上下文时请求通知权限；使用 `userVisibleOnly=true` 和服务器 VAPID 公钥创建 PushSubscription。iOS/iPadOS 16.4 及以上需先将 PWA 加入主屏幕，页面给出相应说明。
2. 将现有 `vite-plugin-pwa` 改为支持自定义 service worker 的构建方式，使同一 worker 处理应用壳预缓存、`push`、`notificationclick` 和更新提示。收到 push 后用 `event.waitUntil` 调用 `showNotification`。
3. `notificationclick` 只打开或聚焦本站 `/events/{eventId}`；路由守卫对未登录用户先进入 `/login` 并保存安全站内回跳，登录后仍由 event-service 校验事件归属。不得使用服务端任意外部 URL 跳转。
4. 静态应用壳可预缓存；`/api`、`/media`、截图和通知内容必须走网络，不将账号敏感数据保存在跨账号可复用的 service worker 缓存中。
5. 用户关闭通知时调用服务端解绑并在浏览器端取消 PushSubscription；UI 明确展示权限拒绝、浏览器不支持、未安装主屏幕和订阅失效等状态。

### 6.2 最终交付物与验收

- push-service 源码、迁移、镜像、接口与恢复说明；PWA 通知设置、service worker 与安装说明。
- 可复现记录覆盖手机 PWA 用户手势订阅、后台收到本账号告警摘要、点击后进入对应详情、登录过期回跳、账号切换隔离、退出撤销和失败重试。
- 通知的地点、危险等级和时间与事件记录一致；截图不进入推送载荷，原生客户端推送不属于本期交付。

## 七、变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-04 | 从总技术规格拆分 push-service 阶段任务，明确订阅、可靠消息、Web Push、PWA 接线与验收。 |
