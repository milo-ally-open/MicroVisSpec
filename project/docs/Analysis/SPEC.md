# 云哨边缘智枢首发技术规格（SPEC）

> 状态：首发技术规格与验收合同；可拆分实施任务，尚未表示代码已实现或已通过验收。
>
> 日期：2026-10-04。
>
> 输入：[PRD](PRD.md)、[调研决策](../Research/RESEARCH.md)、[架构设计](../Design/STRUCTURE.md)、[README](../../../README.md)。产品范围以 PRD 为准；本文件细化接口、数据和运行合同。

> 范围说明：本文件 §§1–10 描述现有 MVP 的单账号基线。三类用户与工作空间属于下一阶段目标，统一目标合同见[三类用户与工作空间授权规格](../Specification/multi_user_workspace_access/)。该组实施并验收前，不能把目标行为当作当前能力；冲突与切换按该组 T-01–T-03 处理。

## 1. 范围与需求追踪

首发运行 auth、device、event、media、push 五个 Go 业务服务，以及 Nginx、PostgreSQL、Mosquitto 和 MediaMTX。mcp-service 仅保留目录和边界，不构建或部署。五个首发 Go 服务分别交付为一个 Linux amd64 二进制，由 systemd 管理；Web/PWA 构建为静态文件，由主机 Nginx 提供。部署及验收状态以对应 `tasks.md` 和[平台部署任务记录](../Specification/platform_deployment/tasks.md)为准。[架构设计 §1–§3](../Design/STRUCTURE.md)

本文件是跨服务的共同合同。各服务按对应规格组 `proposal → spec → design → tasks` 逐项实施与验收；五个 Go 服务的服务内集成测试均单独列入各自 `tasks.md`。边缘、课堂场景和 Web/PWA 的实现与验收任务见同目录领域组；mcp-service 尚待设计，本期不编写实现任务书。

| 服务 | 实现任务书 |
| --- | --- |
| auth-service | [auth_service/tasks.md](../Specification/auth_service/tasks.md) |
| device-service | [device_service/tasks.md](../Specification/device_service/tasks.md) |
| event-service | [event_service/tasks.md](../Specification/event_service/tasks.md) |
| media-service | [media_service/tasks.md](../Specification/media_service/tasks.md) |
| push-service | [push_service/tasks.md](../Specification/push_service/tasks.md) |
| Atlas 边缘运行时 | [atlas_edge_runtime/tasks.md](../Specification/atlas_edge_runtime/tasks.md) |
| 首发教室行为规则 | [classroom_behavior_detection/tasks.md](../Specification/classroom_behavior_detection/tasks.md) |
| Web 与 PWA | [web_pwa_client/tasks.md](../Specification/web_pwa_client/tasks.md) |

| PRD 能力或用户故事 | 首发技术落点 | 验收观察点 |
| --- | --- | --- |
| 注册、登录、个人账号数据归属 | auth-service 账号与会话；Nginx 身份转发；所有业务服务按 account_id 再授权。 | 两个账号相互无法读取或修改对方设备、事件、截图、流与推送订阅。 |
| 旧密码改密并确认新密码 | auth-service 改密接口、旧密码验证、会话撤销事件。 | 错误旧密码和确认不一致被拒绝；改密后旧会话失效。 |
| 手动登记 IP、名称、位置 | device-service 设备登记与一次性激活。 | 用户能登记设备，现场 Atlas 200I DK A2 激活后状态可见。 |
| 查看状态、摄像头、算法、参数并配置受支持参数 | device-service 资产、心跳、算法期望/实报配置及 MQTT 命令回执。 | 在线、算法与视频状态分开显示；配置失败不会显示为已生效。 |
| 桌面实时监控 | media-service 管理路径和观看配额，MediaMTX 拉 RTSP，WebRTC 优先、HLS 回退。 | 最多 12 路活动源流、两个完整 12 分屏桌面会话；手机 PWA 无监控大屏。 |
| 教室检测、异常事件、查询、详情和截图 | Atlas 运行 YOLO11n/640；device-service 管理三类告警规则（默认关闭）；event-service 校验设备与配置版本、幂等落库；证据经 HTTPS 上传。 | 普通检测不创建事件；只有显式启用且满足规则的行为可入库、统计或推送；截图 24 小时过期。 |
| 待处置、处理中、已解除/误报及反馈 | event-service 状态机、版本并发控制、处置历史。 | 每次流转留人员、时间、处理结果；非法或并发过期流转被拒绝。 |
| 手机 PWA 实时通知并跳转详情 | 只有规则校验通过的 event outbox → push-service → Web Push → service worker。 | 仅当前账号订阅收到通知；点击后登录并打开对应详情；默认无规则时不产生课堂告警推送。 |
| 账号范围的基础统计 | device/event 账号级聚合 API + Vue /analytics。 | 今天、近 7 天、近 30 天及约定卡片、图表可展示。 |
| 后续 MCP、原生客户端和更多设备/算法 | mcp-service 占位；原生 App 不在首发交付。 | 首发部署无 MCP 工具接口和原生推送依赖。 |

教室行为检测是首发试点场景，不进入通用表或 API 路径名称。`hand-raising`、`reading`、`writing` 三类检测的异常规则初始全部关闭；只有已显式启用并配置的类别规则才产生异常事件、通知和告警统计。检测行为类别不代表危险等级；类别到等级、置信度阈值与持续条件由规则配置提供。课堂模型不识别或跟踪学生身份，不推断成绩或专注度。消防通道占用检测不属于首发范围。账号共享、成员邀请、对象存储桶、视频录像回放、SSH 业务通信和独立 analysis-service 均不在首发范围。首发以 PRD 的完整业务闭环验收，不另造产品量化指标。[PRD §4–§7](PRD.md)

## 2. 全局技术约定

### 2.1 部署和命名

- 业务服务均采用 Go、Gin、GORM，各自独立二进制进程，由 systemd 管理并绑定 loopback。PostgreSQL 首发单实例，按 auth、device、event、media、push 分 schema、数据库账号与迁移；禁止跨服务直连别人的 schema。[架构设计 §2](../Design/STRUCTURE.md)
- 浏览器 API 统一以 /api/v1 开头。主机 Nginx 提供 Vue 静态文件、HTTPS、路由代理和授权子请求；服务内部接口使用 /internal/v1，仅通过 loopback 可达；设备激活和截图上传使用 /edge/v1 的独立设备认证入口。
- 各实体 ID 为 UUID 字符串；设备侧 event_id、command_id 在首次产生时确定，重试不得更换。服务间只传稳定 ID，不把名称或设备 IP 当关联键。
- API JSON 使用 snake_case 字段。时间戳统一为带时区的 RFC 3339 UTC；数据库用 timestamptz。分析大屏按 Asia/Shanghai 日界线分组。账号 ID 仅从可信会话或内部服务身份取得，不接受前端请求体中的 account_id。
- 一般响应为业务对象或带 items、next_cursor 的列表；错误统一包含 error.code、error.message、request_id。401 表示未登录，403 表示无权，404 表示对象不存在或不属于当前账号，409 表示状态/版本冲突，410 表示证据过期，422 表示内容校验失败，429 表示限流，503 表示依赖暂不可用。外部错误不返回内部文件路径、RTSP 凭据和数据库细节。

### 2.2 身份、会话与服务调用

auth-service 用 Argon2id 保存密码哈希，不存明文密码。用户名统一规范化后建立唯一索引；首发用户名允许 3–32 位 ASCII 字母、数字和下划线，密码至少 12 个字符。注册、登录与设备激活分别限流，错误登录不透露用户名是否存在。会话使用随机不可猜测的 opaque token，仅以哈希存库；浏览器通过 Secure、HttpOnly、SameSite=Lax、Path=/ 的 Cookie 持有。会话闲置 7 天过期；退出或改密立即撤销对应会话。会话到期后，用户打开 PWA 页面或通知详情需重新登录。

Nginx 对受保护 /api/v1 请求调用 auth-service 的内部会话校验，清除客户端自带的身份头后设置已验证的 X-Account-ID 和 X-Session-ID；服务仍须按账号限定每次读写。所有带 Cookie 的写请求校验 Origin，并要求会话关联的 X-CSRF-Token；登录后或页面重载时由 auth-service 向同源前端返回该 token。退出入口由 auth-service 自行验证有效会话及 CSRF，已撤销会话的同源重复退出只清 Cookie 并返回成功，不经过会抢先返回 401 的通用授权子请求。设备入口不用浏览器 Cookie 或 CSRF token，而使用设备凭据。Gin 仅信任 loopback Nginx 反向代理。[Nginx auth_request](https://nginx.org/en/docs/http/ngx_http_auth_request_module.html)、[Gin 可信代理](https://gin-gonic.com/en/docs/server-config/trusted-proxies/)

内部 HTTP 调用带按服务区分、可轮换的服务凭据，Nginx 不将内部接口暴露公网。服务调用设超时并使用稳定幂等键；失败由 outbox 或定期对账补偿。显式退出、改密时 auth-service 在撤销会话的同一事务内写入 session.revoked outbox，异步通知 push-service 停用该会话创建的订阅、media-service 结束该会话的播放。普通会话自然过期不自动取消用户已同意的后台 Push 订阅；通知点击时仍要求重新登录。

### 2.3 数据边界

每个业务服务拥有自己的表、迁移、数据访问账号和写入入口。跨服务 ID 是逻辑引用，不建跨 schema 外键。设备、摄像头、算法能力以 device-service 为准；事件保存发生时的名称、位置、算法版本等快照；media-service 独占流来源与凭据；push-service 独占浏览器订阅。所有列表、详情、统计、证据和媒体授权都按 account_id 过滤，不能只依赖前端隐藏按钮。

## 3. 对外接口合同

下列路径是首发的资源和责任归属，字段细节可据此生成 OpenAPI；所有修改接口要求 JSON 请求体，截图上传除外。浏览器不直接调用服务内部端口或 MediaMTX 控制 API。

### 3.1 auth-service

| 方法与路径 | 输入和结果 | 规则 |
| --- | --- | --- |
| POST /api/v1/auth/register | username、password；返回 account_id、username。 | 用户名规范化唯一；注册不自动登录。 |
| POST /api/v1/auth/login | username、password；返回账号基本资料与 csrf_token，设置会话 Cookie。 | 错误凭据统一响应，记录审计与限流。 |
| GET /api/v1/auth/me | 返回 account_id、username、csrf_token。 | 未登录返回 401；页面刷新后恢复登录态。 |
| POST /api/v1/auth/logout | 无业务字段；撤销当前会话并清 Cookie。 | 重复退出返回成功；同事务写 session.revoked outbox。 |
| PUT /api/v1/auth/password | old_password、new_password、new_password_confirmation。 | 校验旧密码与确认值；成功后撤销该账号所有旧会话，要求重新登录。 |

登录会话不作为 Push 推送的永久授权凭据。订阅时记录当前 account_id 和 session_id，普通会话自然过期后订阅仍有效；显式退出、改密或设备换账号时禁用受影响的订阅。这样后台通知可持续到达，同时通知详情仍要经过新会话鉴权。

### 3.2 device-service

| 方法与路径 | 输入和结果 | 规则 |
| --- | --- | --- |
| POST /api/v1/devices | name、ip、location；返回 device_id、pending 状态。 | 由当前账号持有；IP 仅为资产地址，登记不等于在线。 |
| GET /api/v1/devices；GET /api/v1/devices/{deviceId} | 返回当前账号设备、摄像头、算法、心跳与配置状态。 | 列表分页；设备在线、算法运行、媒体可播放分别展示。 |
| PATCH /api/v1/devices/{deviceId} | 修改 name、ip、location。 | 不允许修改 owner account_id；位置变化不覆盖历史事件快照。 |
| DELETE /api/v1/devices/{deviceId} | 停用并从当前设备列表移除。 | 同事务写 device.disabled 通知；撤销 MQTT 凭据、禁用媒体来源；历史事件仍按留存规则可查。 |
| POST /api/v1/devices/{deviceId}/activation-tickets | 返回只显示一次的激活凭据与过期时间。 | 凭据哈希入库、10 分钟有效，重新签发使旧凭据失效。 |
| POST /api/v1/devices/{deviceId}/cameras | name、camera_key；返回 camera_id。 | camera_key 在设备内唯一；媒体源另由 media-service 配置。 |
| GET /api/v1/devices/{deviceId}/algorithms | 算法名称、版本、运行状态、受支持参数与实报值。 | 参数能力以边缘实报为准。 |
| PUT /api/v1/devices/{deviceId}/algorithms/{assignmentId}/parameters | expected_version、parameters；返回新期望版本和 pending 状态。 | 校验允许的参数；命令回执成功后才标记 applied。 |
| GET /api/v1/devices/stats | registered、online、as_of。 | 仅当前账号，online 依据心跳计算。 |

边缘代理使用 POST /edge/v1/activate 携带一次性凭据与 device_id，device-service 核对后发放唯一 MQTT client_id 与一次显示的设备密码，并通过 Mosquitto 的安全管理能力写入该设备的 ACL。设备激活与 MQTT 凭据均可撤销；重新激活须由账号用户重新签发凭据。设备状态心跳默认每 30 秒一次，超过 90 秒未收到有效心跳判定离线；离线不代表摄像头或媒体一定不可达。当前 Web 设备详情页还通过受限 SSH 向导完成首次引导接入；SSH 不承载常规业务通信，设备状态、事件和命令走 HTTPS/MQTT。三角色目标下保留此指纹确认和任务状态 UI，另新增工作空间成员对设备能力的授权流程；这是两种不同的“设备授权”。[Mosquitto Dynamic Security](https://mosquitto.org/documentation/dynamic-security/)

算法参数遵循“期望版本 → MQTT 命令 → 设备 ACK/实报版本”的异步过程。UI 不将请求成功解释成边缘已生效；失败、超时和设备不支持都保留原因。没有能力描述的参数仅可查看，不可编辑。

首发课堂算法键为 `classroom_behavior_detection`，模型流程固定为 YOLO11n/640，类别 allowlist 为 `hand-raising`、`reading`、`writing`。device-service 保存每个不可变配置版本及设备 ACK 生效时间；新设备/新配置中三类 `enabled` 均为 `false`。启用某类规则时必须配置 `[0,1]` 置信度阈值、正值持续时间、非负冷却时间和 `low|medium|high|critical` 事件等级。没有配置值时不得启用；规则版本仅在设备回报已应用后可产生事件。

event-service 在 MQTT 入站后通过服务凭据调用 device-service 的 `POST /internal/v1/devices/{deviceId}/classroom-events/validate`，请求含 `camera_id`、`algorithm_version`、`rule_config_version`、`behavior_class`、`confidence`、`observed_duration_ms`、`severity`、`occurred_at`。允许结果返回可信账号、设备/摄像头名称和位置快照以及被校验配置版本；设备/摄像头不存在或已停用返回永久拒绝，规则未启用/阈值不符返回规则拒绝，归属/历史版本依赖暂时不可用返回可重试服务错误。边缘不传可信 `account_id`，event-service 不对拒绝或暂时错误生成 `event.created`。

### 3.3 event-service

| 方法与路径 | 输入和结果 | 规则 |
| --- | --- | --- |
| GET /api/v1/events | 时间、type、severity、device_id、camera_id、location、status、cursor、limit；返回 items 与 next_cursor。 | 默认近 7 天、倒序；limit 默认 20、最大 100。 |
| GET /api/v1/events/{eventId} | 返回事件、设备/位置快照、截图状态和处置历史。 | 仅当前账号；截图过期仍返回事件内容。 |
| GET /api/v1/events/{eventId}/evidence | 返回图片字节。 | 待上传返回 404 与明确状态；过期返回 410；不公开文件路径。 |
| POST /api/v1/events/{eventId}/transitions | to_status、result、expected_version；返回新状态、版本与处理时间。 | 服务端检查合法状态流转；终态必须有处理结果。 |
| GET /api/v1/events/stats | range=today、7d 或 30d；返回卡片与各图表聚合及 as_of。 | 时段按 Asia/Shanghai 分组，仅当前账号。 |
| PUT /edge/v1/devices/{deviceId}/events/{eventId}/evidence | JPEG 或 PNG 图片；返回证据状态与 expires_at。 | 此处 eventId 是边缘生成的 event_id，以 (device_id,edge_event_id) 定位平台事件；设备身份、事件归属和大小校验；同内容重传幂等。 |

事件列表固定按 (occurred_at DESC, id DESC) 排序。cursor 是包含末条排序键、筛选条件摘要和有效期的签名不透明字符串；筛选条件改变时旧 cursor 无效。所有查询强制 account_id 条件。允许的危险等级为 low、medium、high、critical；允许的处置状态为 pending、processing、resolved、false_positive，中文界面分别显示待处置、处理中、已解除、误报。

状态流转允许 pending → processing、pending → false_positive、processing → resolved、processing → false_positive；终态不可直接再变更。expected_version 与当前版本不符返回 409，处理人取当前账号，服务器记录操作时间和完整历史。事件的发生时间由边缘上报，平台另外记录接收时间；设备时钟明显异常时记录告警供排查，不静默改写原始发生时间。

边缘截图单文件最多 4 MiB，存储路径由服务生成，限制 MIME、文件签名和尺寸；数据库只保存相对路径与 SHA-256。上传成功时写入 uploaded_at 和 expires_at=uploaded_at+24 小时。过期读取立即返回 410，后台每小时清理文件；已闭环事件与处置记录自进入终态起保留 90 天，未闭环事件持续保留。截图文件不做长期备份，恢复后缺失的证据应显式显示不可用。[架构设计 §5–§6](../Design/STRUCTURE.md)

### 3.4 media-service 与 MediaMTX

| 方法与路径 | 输入和结果 | 规则 |
| --- | --- | --- |
| PUT /api/v1/media/cameras/{cameraId}/source | RTSP URL、独立的 source_username/source_password；返回 stream_id、config_status。 | 核对摄像头归属；凭据加密保存，不允许 URL 内嵌密码。 |
| GET /api/v1/media/cameras/{cameraId}/playback | 返回受控的 whep_url、hls_url、stream_id 与当前状态。 | 核对账号，分配短期观看租约；不返回原始 RTSP URL。 |
| GET /api/v1/media/cameras/{cameraId}/status | 返回源可达、拉流、编码兼容与播放状态。 | 从 MediaMTX 状态及最近错误归并，不以设备心跳代替。 |
| POST /api/v1/media/viewer-leases/{leaseId}/renew；DELETE 同路径 | 续租或释放观看连接。 | WebRTC 播放页周期续租；过期或撤销后关闭对应连接。 |

media-service 将配置与 MediaMTX 实际路径对账，首发路径为 opaque stream_id，RTSP source 按观看需求拉取，显式开启 sourceOnDemand。MediaMTX 的控制 API、RTSP 入口及来源凭据留在私有网络。公网只经 Nginx 提供受保护的 /media/{streamId}/whep 和 /media/{streamId}/index.m3u8；Nginx 对每个信令、播放列表及分片请求调用 media-service 授权子请求。媒体授权同时检查会话和摄像头归属；MediaMTX 的直接 HTTP 读取需要内部身份，不保留匿名读权限。HLS 播放组件必须让分片请求携带同源 Cookie，不用无法附带鉴权信息的公开 iframe。[MediaMTX 配置](https://mediamtx.org/docs/references/configuration-file)、[鉴权](https://mediamtx.org/docs/features/authentication)、[浏览器播放](https://mediamtx.org/docs/read/web-browsers)

媒体准入限制全平台最多 12 条活动源流和 24 个观看连接，其中一个完整 12 分屏桌面会话占 12 个观看连接。租约为 30 秒，播放器每 15 秒续租；异常关闭通过租约过期与 MediaMTX 会话对账回收。显式退出或改密撤销会话时，media-service 终止关联的 WebRTC/HLS 会话；不能只依靠握手时的一次 HTTP 鉴权。首发 Atlas 200I DK A2 每台默认一路 720p/15 fps、H.264 Baseline 无 B 帧、1.5 Mb/s 的处理后 RTSP，MediaMTX 不承担重编码。园区内网 WebRTC 使用 UDP 8189，失败或园区外桌面访问回退 HTTPS HLS，不部署 STUN/TURN。[架构设计 §5.2](../Design/STRUCTURE.md)

### 3.5 push-service 与 PWA

| 方法与路径 | 输入和结果 | 规则 |
| --- | --- | --- |
| GET /api/v1/push/vapid-public-key | 返回 VAPID 公钥。 | 私钥只在服务器机密中保存。 |
| PUT /api/v1/push/subscriptions | endpoint、keys.p256dh、keys.auth；返回 subscription_id。 | 仅在用户主动授权后提交；endpoint 全局唯一，换账号时原绑定失效。 |
| GET /api/v1/push/subscriptions | 返回当前账号/浏览器的订阅状态。 | 不返回密钥材料。 |
| DELETE /api/v1/push/subscriptions/{subscriptionId} | 停用订阅。 | 仅绑定账号可操作；重复停用成功。 |

登录用户点击启用通知后，浏览器以 userVisibleOnly=true 和 VAPID 公钥调用 PushManager.subscribe，再把 endpoint 与密钥上传；服务端保存账号、创建时会话、订阅和投递状态。每次成功登录、恢复会话及进入通知设置页时，前端核对已有浏览器订阅并按需重新登记，使同一 endpoint 绑定当前会话并处理浏览器订阅变化；该同步不重新弹出权限请求。登出时前端先停用当前订阅，auth-service 的 session.revoked 事件作为服务端补偿；普通会话自然过期不影响已同意的后台推送。iOS/iPadOS 16.4 及以上须先将 PWA 加入主屏幕，订阅必须由用户手势触发。[PushManager.subscribe](https://developer.mozilla.org/en-US/docs/Web/API/PushManager/subscribe)、[Apple Web Push](https://developer.apple.com/documentation/usernotifications/sending-web-push-notifications-in-web-apps-and-browsers)

push-service 接收 event.created 后对 event_id + subscription_id 建唯一投递记录，通知包含位置、危险等级、发生时间和 /events/{eventId} 深链，不包含截图。service worker 的 push 处理必须调用 showNotification；notificationclick 打开或聚焦详情页，目标页再走账号鉴权。推送服务接受消息只记作 accepted，不记作“用户已收到/已读”；失效订阅响应停用，临时错误退避重试，告警始终可在事件列表查询。[Web Push RFC 8030](https://www.rfc-editor.org/rfc/rfc8030.html)

## 4. 边缘设备与服务间消息合同

### 4.1 MQTT 主题与设备权限

首发使用 Mosquitto 2.x、MQTT 3.1.1、TLS、每台设备独立 client_id 和凭据。固定 client_id 的设备使用持久会话；broker 显式启用持久化，限制连接数、报文大小和离线队列，防止断网堆积撑满磁盘。事件元数据与命令采用 QoS 1，事件不设置 retained；在线状态可用 retained + LWT 辅助，但最终在线判定仍取 device-service 的心跳时间。[MQTT 3.1.1](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/mqtt-v3.1.1.html)、[Mosquitto 配置](https://mosquitto.org/man/mosquitto-conf-5.html)

| 主题 | 方向 | 内容与 ACL |
| --- | --- | --- |
| devices/{deviceId}/state | 设备发布，device-service 订阅 | 心跳、运行状态、摄像头/算法实报；设备只可发布自己的主题。 |
| devices/{deviceId}/events | 设备发布，event-service 订阅 | 通用异常事件元数据；不带截图二进制。 |
| devices/{deviceId}/event-acks | event-service 发布，设备订阅 | 应用结果包含 `event_id`、`result=accepted|duplicate|rejected` 及稳定 reason_code；仅 accepted/duplicate 表示服务端事务已提交。 |
| devices/{deviceId}/commands | device-service 发布，设备订阅 | 有 command_id、期望配置版本的受支持参数命令。 |
| devices/{deviceId}/command-acks | 设备发布，device-service 订阅 | command_id、执行结果、实报版本及失败原因。 |

所有消息含 schema_version=1、device_id、sent_at；event 消息还含 event_id、camera_id、event_type、severity、occurred_at、algorithm_key、algorithm_version、可选 region_key 和受限的 attributes。教室事件的 `event_type` 只允许 `classroom.behavior.hand-raising`、`classroom.behavior.reading`、`classroom.behavior.writing`；attributes 需提供 `behavior_class`、`confidence`、`observed_duration_ms` 和 `rule_config_version`；device_id 必须与 MQTT 身份和主题一致；account_id 即使由设备携带也不作为可信归属。单条 MQTT JSON 元数据上限 64 KiB；图片只走 HTTPS 证据接口。

边缘算法对一次规则触发只生成一个 event_id，同一事件的重试沿用该 ID。event-service 以 (device_id,event_id) 唯一约束做确定性去重；接收教室事件前还须向 device-service 校验该规则配置版本已经在该设备生效、对应类别已启用、置信度/持续时间达到阈值且上报等级与规则一致。未知、未生效、已禁用或字段不符的规则事件拒绝入库；device-service 对配置版本保留只读快照，至少覆盖引用它的事件留存周期。QoS 1 的 broker PUBACK 只代表 broker 接收；设备本地持久化未得到 event-acks 的事件，断线后重发，直到 event-service 完成数据库事务并发送应用级 ACK。重复事件返回同一结果，不重复产生 event.created。[MQTT 3.1.1](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/mqtt-v3.1.1.html)

`accepted` 和 `duplicate` 是正向应用 ACK；永久 schema/规则拒绝返回 `rejected` 与稳定 reason_code，边缘转入本地 quarantine 状态并保留记录，不再无限重发，待设备状态/配置修复后由受控操作决定是否重新提交。数据库、device-service 或 broker 依赖暂时不可用时不发送终结 ACK，边缘保留并退避重试。event-service 不得把 broker PUBACK 当作业务确认。

命令使用 command_id 和 expected_version 幂等执行；设备报告 applied、rejected 或 failed，device-service 保存期望与实报版本。设备离线时仅保留仍有意义的待执行命令；过期命令不能在设备恢复后悄悄应用。边缘本地未确认事件和待上传截图放入有界持久队列，容量不足时上报降级状态并告警，不能把消息成功发送到 broker 等同于处置完成。

### 4.2 服务间可靠事件

event-service 在同一 PostgreSQL 事务内写入新事件与 event.created outbox；auth-service 在同一事务内撤销会话与写入 session.revoked outbox；device-service 在同一事务内停用设备与写入 device.disabled outbox。后台 worker 领取待投递记录，通过仅内网可达的 /internal/v1/inbox 交付给目标服务；请求附服务凭据、event_type、event_id、schema_version、occurred_at 和负载。接收方以 (producer,event_id,event_type) 唯一约束写 inbox，重复交付返回成功但不重复执行。event.created 负载只包含 account_id、event_id、地点、危险等级、发生时间与详情路径；session.revoked 包含 account_id、session_id，不带密码或浏览器密钥；device.disabled 包含 account_id、device_id，由 media-service 停用该设备关联流及观看租约。device-service 还须重试 broker 凭据撤销，event-service 在归属校验时拒绝已停用设备的后续事件。

outbox worker 使用 PostgreSQL 行锁领取任务，提交时独立于业务事务；暂时失败按退避计划重试，worker 中断后租约过期可再领取。对已重复消费的消息返回成功，避免无穷重试。投递长期失败需保留记录并告警，不能静默删除。push-service 对单一事件按有效订阅逐条投递，404/410 类型的永久失效响应停用订阅，429/5xx 等暂时错误重试；通知过旧时停止继续发送并记录原因。服务端记录的是“推送服务已接受”，不是用户已阅读。[GORM 事务](https://gorm.io/docs/transactions.html)、[PostgreSQL SKIP LOCKED](https://www.postgresql.org/docs/current/sql-select.html)

服务间信封的 `event_id` 用于 inbox 幂等：`event.created` 直接使用平台内部事件 ID，`session.revoked` 和 `device.disabled` 使用各自 outbox 中稳定生成的 UUID；重投保持原值。边缘 MQTT 的 `event_id` 另称 `edge_event_id`，只在 `(device_id,edge_event_id)` 范围内去重。

### 4.3 截图文件与事件入库顺序

event-service 先保存事件元数据，截图随后可独立上传。文件先写入私有卷临时路径，校验格式、大小及哈希后原子重命名；数据库再记录截图元数据。若文件写入成功而事务失败，定期任务删除孤儿文件；若数据库记录存在但文件缺失，详情返回证据不可用。重复上传同一 event_id 和同一内容返回已有 expires_at；内容不同返回 409，不覆盖已保存证据。图片不作为 Nginx 静态资源，不进 service worker 缓存。

## 5. 数据模型、索引与生命周期

| schema | 最小表/实体 | 关键字段与约束 |
| --- | --- | --- |
| auth | accounts | id、username_normalized 唯一、password_hash、created_at、updated_at。 |
| auth | sessions、outbox | sessions：token_hash 唯一、account_id、created_at、last_seen_at、expires_at、revoked_at；outbox：事件 ID 唯一与投递状态。 |
| device | devices、outbox | devices：id、account_id、name、ip、location、activation_state、last_seen_at、reported_state、created_at；(account_id,id) 查询索引；outbox：device.disabled 与投递状态。 |
| device | cameras、algorithm_assignments、activation_tickets | camera_id、device_id、camera_key；算法键/版本、参数 schema、desired/reported JSON 与版本；激活票据仅存哈希和到期时间。 |
| event | events | id、account_id、device_id、camera_id、edge_event_id、event_type、severity、occurred_at、received_at、位置/名称/算法快照、status、version；(device_id,edge_event_id) 唯一。 |
| event | evidence、dispositions、outbox | evidence：event_id 唯一、relative_path、mime、sha256、uploaded_at、expires_at、deleted_at；dispositions：actor_account_id、from/to 状态、result、acted_at；outbox：event.created。 |
| media | streams、viewer_leases、inbox | streams：stream_id、account_id、device_id、camera_id 唯一、加密的 RTSP 来源/凭据、config_version、status；leases：session_id、stream_id、expires_at；inbox：session.revoked、device.disabled 去重。 |
| push | subscriptions、inbox、deliveries | endpoint 唯一、account_id、source_session_id、keys 加密值、disabled_at；inbox 事件唯一；deliveries 对 (event_id,subscription_id) 唯一并记录尝试/结果。 |

event.events 建 (account_id,occurred_at DESC,id DESC) 主列表索引，以及账号+状态、账号+设备/摄像头、账号+类型/等级与时间的筛选索引；聚合按账号及发生时间使用这些索引。终态事件 90 天清理和截图 24 小时清理均按时间索引批量执行，避免一次大事务。统计按事件快照位置而不是当前设备位置，数据删除后不再计入相应统计窗口。服务各自维护迁移，先增加兼容字段、迁移数据、切换读写，最后清理旧字段；跨服务变更先兼容旧消息版本。

前端默认查询最近 7 天，事件元数据和处置记录的保留期为终态后 90 天，截图保留期是上传后 24 小时。未闭环事件不因达到 90 天自动删除，但其截图仍按 24 小时清理。数据库备份包含账号、设备、事件、处置及证据状态，不长期备份截图字节；恢复后要重新检查 evidence 的文件存在性并向用户明确显示缺失或过期。[架构设计 §6](../Design/STRUCTURE.md)

## 6. Web/PWA 实现合同

前端仍使用同一个 Vue 3 + TypeScript 工程 [project/src/clients/web](../../src/clients/web/README.md)，页面放在 src/pages，路由集中在 src/router，类型化请求客户端放在 src/api。当前代码路由为 /login、/register、/、/devices、/devices/:deviceId、/events、/events/:eventId、/monitor、/analytics、/settings；用户设置含密码和 Push 订阅管理。设备详情页现有设备登记后的首次 SSH 引导/指纹确认/进度、激活续期、摄像头与 RTSP 来源管理；/monitor 最多渲染 12 个 CameraPlayer，窄屏 CSS 仅把栅格改为单列，每个 CameraPlayer 挂载时都会申请自己的观看租约。这些均按统一个人账号，不构成 admin/PC/mobile 路由或成员授权。目标 /admin、/app、/m 及迁移兼容路由由[三类用户规格组 REQ-08](../Specification/multi_user_workspace_access/spec.md#req-08-角色化-webpwa-导航)定义；移动成员单路播放器需确保只有所选摄像头持有活动租约；/mcp 暂不启用。[架构设计 §4](../Design/STRUCTURE.md)

路由守卫调用 /api/v1/auth/me 恢复会话；未登录访问受保护页面时保存安全的站内目标路径，登录后返回原页。通知点击仅允许 /events/{eventId} 这样的站内路径，不接受服务端返回的任意外部跳转地址。API 客户端在写请求附 X-CSRF-Token，遇 401 导向登录并保留回跳，遇 409 提示刷新冲突状态，截图 410 显示“截图已过期”。/analytics 将 device/event 两份统计按 as_of 分别标注，不在客户端用其他账号数据补齐缺项。

现有 vite-plugin-pwa 配置生成应用壳缓存，但没有 Push 事件处理。首发接入时改为该插件支持的自定义 service worker 构建，使一个 worker 同时处理预缓存、push、notificationclick 和更新提示；保留“提示更新”行为。只预缓存静态应用资源，/api、/media、截图、通知内容和 MCP 路径始终走网络，避免账号切换后读到旧内容。Service worker 从推送负载取得 event_id，调用 showNotification 并以 event.waitUntil 保证处理；notificationclick 打开或聚焦事件详情。应用安装、通知权限和订阅必须向用户明确展示。[vite-plugin-pwa injectManifest](https://vite-pwa-org.netlify.app/guide/inject-manifest)、[Push API](https://www.w3.org/TR/push-api/)

监控大屏按当前账号设备和摄像头加载最多 12 格，先请求 media-service 的播放信息再启动 WebRTC。窄屏只把栅格改成单列；每个挂载的 CameraPlayer 都会申请媒体租约，当前页面仍可能同时建立多路连接。播放器明确显示连接中、拉流失败、编码不兼容、额度已满和 HLS 回退；组件卸载时释放观看租约。现有窄屏布局没有移动成员/设备 grant 语义，也不保证单路播放；未来 PWA 必须按服务端当前授权筛选设备和摄像头，并只为用户选中的摄像头创建播放器，切换时先释放旧租约。统计页进入及切换今天/近 7 天/近 30 天时刷新，前台每 60 秒刷新；事件详情在状态流转后重新拉取服务端结果。PWA 仅保留离线应用壳，不承诺离线设备、事件或媒体数据。

## 7. 运行质量、安全与部署

### 7.1 首发容量与质量界线

| 指标 | 首发设计值或行为 |
| --- | --- |
| 视频源与观看 | 全平台最多 12 路活动源、24 个观看连接，相当于两个同时完整观看的桌面 12 分屏。 |
| 边缘视频 | Atlas 200I DK A2 每台默认 1 路处理后 RTSP；H.264 Baseline、无 B 帧、720p、15 fps、目标 1.5 Mb/s、约 2 秒关键帧间隔。 |
| 网络预算 | 12 路视频净入站约 18 Mb/s；两个完整大屏净出站约 36 Mb/s，另留协议余量。服务器内网千兆；若两个大屏通过公网 HLS 观看，园区公网可用上行至少 100 Mb/s。 |
| MQTT 与告警 | 设备心跳 30 秒，90 秒未见心跳判离线；事件 QoS 1 加业务 ACK 和唯一键去重。 |
| 查询与统计 | 告警默认近 7 天、每页 20/最多 100；统计支持今天、近 7 天、近 30 天，前台每 60 秒刷新。 |
| 留存 | 截图上传后 24 小时到期；已闭环事件及处置记录终态后 90 天，未闭环记录保留至闭环。 |

视频参数是 MVP 的资源配置与验收目标，Atlas 200I DK A2 的标称编码能力不能替代实际“采集、推理、叠加、编码”整链路测量。首发联调需用目标设备与园区网络验证 12 路拉取、两个大屏观看和 WebRTC 失败后 HLS 可用；PRD 未设置额外的通知送达率或事件处理时效产品承诺。[Atlas 200I DK A2 VENC 应用开发指南](https://www.hiascend.com/document/detail/zh/Atlas200IDKA2DeveloperKit/23.0.RC2/Application%20Development%20Guide/aadgc/aclcppdevg_000033.html)、[MediaMTX WebRTC 要求](https://mediamtx.org/docs/features/webrtc-specific-features)

### 7.2 安全边界

- 公网只开放 Nginx HTTPS；园区内网允许 Atlas 200I DK A2 到 MQTT/TLS、后端到 RTSP 源、桌面到 MediaMTX UDP 8189。PostgreSQL、Mosquitto 管理口、MediaMTX 控制 API、Go 服务内部端口和截图卷不公开。[架构设计 §2](../Design/STRUCTURE.md)
- MQTT 设备按 client_id 和主题 ACL 限制发布/订阅。设备 IP、payload 中的 account_id 和 RTSP URL 都不能作为身份凭据。设备注销时撤销 broker 凭据、停止媒体流，历史事件按留存规则保存。
- Nginx 校验浏览器会话和 Origin，清理伪造的 X-Account-ID 等身份头；各服务按账号与资源 ID 二次授权。对媒体请求同样执行鉴权，不能只保护播放信息 API。已建立的 WebRTC 会话由 media-service 租约和会话撤销管理。[MediaMTX 鉴权](https://mediamtx.org/docs/features/authentication)
- 密码哈希、会话 token 哈希、设备激活凭据哈希、RTSP 凭据加密值和 Push 订阅密钥分别按用途保存；加密密钥、VAPID 私钥、服务凭据来自部署机密，不写入仓库、镜像和日志。截图路径、防路径穿越、MIME 与文件签名校验、最大文件大小均由 event-service 执行。
- 审计至少保留登录失败、设备激活/撤销、算法参数修改、事件处置、媒体源修改和 Push 订阅解绑。日志不记密码、Cookie、VAPID 私钥、RTSP 明文、Web Push endpoint 或截图文件绝对路径。

### 7.3 部署、健康与恢复

首发在一个园区后端服务器运行系统 Nginx、五个 Go 二进制、MediaMTX、Mosquitto 和 PostgreSQL；mcp-service 不构建。Go 服务由 systemd 管理并分别绑定 loopback 端口；Web/PWA 以静态文件方式部署。原生运行、端口隔离、机密文件注入与数据恢复按[平台部署规格](../Specification/platform_deployment/spec.md)验收。各服务提供 `/health/live` 与 `/health/ready`；ready 检查其必需的数据库或基础设施依赖，不能因为可选统计接口暂时失败而误判所有服务不可用。

PostgreSQL 做定期备份并验证恢复；Mosquitto 持久化与安全配置、MediaMTX 路径配置也需备份。截图卷仅保留 24 小时内容，不做长期备份；恢复后 event-service 对数据库证据记录和文件实际存在性对账。数据库迁移由所属服务执行，采用先兼容、后切换、再清理的顺序，禁止多个服务同时修改同一表。

观测指标至少包括设备最后心跳与离线数、MQTT 消费积压、事件入库和重复数量、截图上传失败/到期清理、outbox 未投递量、Push 接受/失败/失效订阅、活动源与观看连接、WebRTC 失败和 HLS 回退、数据库错误、文件卷剩余空间。请求 ID 与 event_id 贯穿边缘、事件、证据和推送日志。磁盘接近阈值、事件/通知积压、媒体源不可达时必须告警；不可用时不伪造业务成功 ACK。

## 8. 技术取舍与风险

| 选择 | 采用原因 | 本期未采用的方案及原因 |
| --- | --- | --- |
| 五个运行中的 Go 服务，mcp-service 后置 | 与已确认的业务边界和 Go/Gin/GORM 栈一致，服务数据归属清晰。 | 一开始部署 MCP：工具权限和审计尚无产品范围，会扩大发布面。 |
| PostgreSQL 单实例、分 schema/账号 | 首发运维成本可控，仍保留服务数据隔离及事务 outbox。 | 每服务独立数据库实例：当前 12 路规模没有足够收益，备份和运维成本更高。 |
| Mosquitto + MQTT 3.1.1 控制面 | 边缘设备可主动连接、离线重连，适合状态/事件/命令。 | SSH 作为常规设备业务通信：连接方向、可恢复性和消息确认不符合设备常规上报；SSH 只用于受限首次 provisioning 或人工维护。 |
| MediaMTX 数据面 + media-service 控制面 | 复用成熟 RTSP 拉流与 WebRTC/HLS，业务层保留账号授权、来源配置和观看配额。 | Go 服务自行转发或统一软件转码：媒体协议与 12 路处理复杂度更高；MVP 在 Atlas 200I DK A2 侧生成兼容码流。 |
| Atlas 200I DK A2 输出 H.264 Baseline 预览流 | 与主流浏览器 WebRTC 的无 B 帧要求匹配，12 分屏按 720p/15 fps 控制解码负载。 | H.265 作为首发浏览器流：WebRTC 浏览器支持范围较窄；统一后端转码会增加资源与部署复杂度。 |
| PostgreSQL outbox + 内部投递 | 事件落库与通知意图同事务，首发不必维护第三套消息基础设施。 | 直接在请求事务里调用 Push 或引入 Kafka/NATS：前者可能丢通知或拖慢入库，后者增加部署和运维。 |
| Web/PWA 共用 Vue 客户端 | 首发满足桌面管理与手机后台通知，并复用页面与账号体系。 | 本期分别制作原生 App：超出已确认的客户端范围。 |
| 统计由 device/event 聚合 | 数据由原所有者计算，前端可组合账号级图表。 | 独立 analysis-service：基础统计阶段会复制数据与增加一致性成本。 |
| 后端私有文件卷、截图 24 小时 | MVP 数据量可控且符合已确定的短期证据用途。 | 本期对象存储与长期截图归档：超出首发范围；服务器故障可能丢失未过期图片，UI 必须说明。 |

主要剩余实施风险是：边缘算法在 Atlas 200I DK A2 上的真实帧率与编码负载、浏览器 12 分屏的解码性能、园区公网 HLS 上行带宽，以及不同浏览器的 PWA 推送行为。这些是既定方案的联调验证项，不作为悬而未决的产品范围。若实机无法满足两屏观看，应先定位边缘推理、媒体分发、网络或客户端瓶颈，再按实测结果调整码率、分辨率或观看预算；不能用 Atlas 200I DK A2 编码器标称能力代替证据。

## 9. 首发验收清单

| 链路 | 必须观察到的结果 |
| --- | --- |
| 账号与隔离 | A/B 两个账号可独立注册登录；改密需旧密码并撤销旧会话；跨账号设备、事件、截图、媒体和订阅请求被拒绝。 |
| 设备接入 | Atlas 200I DK A2 由一次性凭据激活并连入 MQTT；心跳状态、摄像头、YOLO11n/640 模型和课堂规则版本可查看；离线后状态更新，命令以实报结果为准。 |
| 告警默认行为 | 三类告警规则初始均关闭；无有效配置时实时检测只用于预览，不产生事件、截图、统计或推送。 |
| 视频 | MediaMTX 从边缘拉取处理后 RTSP；桌面 WebRTC 可看画面及状态；WebRTC 不通时 HLS 可播放；12 路活动源、两屏观看和超额提示按配置执行。 |
| 教室规则与事件 | 初始规则全关闭时普通检测不产生事件；用户配置并由设备应用的规则命中后，event-service 校验类别、阈值、持续条件、规则版本和等级；重复 QoS 1 上报不产生第二条。 |
| 事件与截图 | 通过规则校验的事件可查询和处置；截图在 24 小时内可看，过期后返回 410，事件详情仍存在。 |
| 处置 | 待处置 → 处理中 → 已解除或误报；记录处理账号、时间与结果；非法和并发冲突请求被拒绝。 |
| PWA 推送 | 仅已启用课堂规则产生的事件可以创建通知；用户手势订阅后，手机后台收到本账号告警摘要；点击进入详情，必要时先登录；显式退出后该订阅不继续接收。 |
| 统计 | 今天、近 7 天、近 30 天可切换；设备卡片、事件趋势/等级/类型/位置/状态图只含当前账号数据。 |
| 延后边界 | 消防通道检测、MCP 工具、原生客户端推送、成员邀请、对象存储与录像回放在首发部署中不存在。 |

## 10. 状态与变更记录

本规格覆盖 PRD 首发用户故事、服务边界、接口、消息、数据模型、运行质量与验收。首发场景、模型流程和规则默认行为已由用户确认；YOLO11n/640 权重与 Atlas OM 产物尚未提供，阻塞真实模型装载、精度和性能验收，但不阻塞服务框架与界面规格。具体字段长度、错误码枚举、数据库迁移脚本及 OpenAPI 文件由实现任务按本规格生成并评审。

| 日期 | 变更 |
| --- | --- |
| 2026-10-04 | 依据 PRD、调研与架构设计建立首发 SPEC，明确五个运行服务和 MCP 占位、Web/PWA、Atlas 200I DK A2/MQTT/媒体、事件/证据、Push、统计、留存、部署及验收合同。 |
| 2026-10-04 | 增加五份服务实现任务书索引；mcp-service 继续保持待设计状态。 |
| 2026-10-15 | 首发边缘设备改为华为 Atlas 200I DK A2（Ascend 310B4），模型链路改为 CANN ATC/OM/AscendCL。 |
| 2026-10-29 | 新增三类用户目标入口及代码分层边界；目标未实现，细节由跨服务规格与系统结构维护。 |
| 2026-10-18 | 首发场景改为教室行为检测，采用 YOLO11n/640；三类告警规则默认关闭，只有设备已应用且命中显式配置的规则才能产生事件。新增 Atlas、课堂场景和 Web/PWA 规格组，五个服务均细分服务内集成测试任务；模型产物后续提供。 |

## 11. 三类用户与工作空间扩展目标

本节只为 PRD 中的下一阶段目标建立入口，详细身份、授权、数据迁移、错误语义和验收条件由[跨服务规格组](../Specification/multi_user_workspace_access/)唯一维护。本节不改写当前 §§1–10 的 MVP 行为或证明实现完成。

目标增加 `admin`、`pc`、`mobile` 三类服务端身份；PC 用户拥有一个工作空间，移动用户经邀请加入一个工作空间，资源授权按设备与能力执行；管理员管理平台账号并查看运行元数据，不默认获得客户 RTSP、视频或截图访问。目标规格保留现有五个服务、Nginx、PostgreSQL 各服务独立 schema、Atlas/MQTT、RTSP/MediaMTX、WebRTC/HLS、Push outbox 和 24 小时截图边界。

代码现状与目标差异、产品待确认项和回滚顺序见该规格组。落实前，当前单账号、`account_id` 隔离及无成员邀请仍是现有服务规格的基线。

## 12. 代码分层与依赖约束（下一阶段目标）

本节描述目标代码边界，不表示仓库已完成分层。现状证据、目录映射、分层图和渐进迁移约束见[系统结构 §12](../Design/STRUCTURE.md#12-当前源码层次与目标代码边界)；三类用户改造中的分层验收见[跨服务规格 REQ-10](../Specification/multi_user_workspace_access/spec.md#req-10-代码边界与分层依赖)。

每个 Go 服务独立演进：cmd/service-name 是组合根；HTTP/MQTT/worker 等入站适配器把协议转换为 application 用例；application 定义用例和出站端口；domain 保存本域实体、值对象、策略与状态机；出站适配器实现端口并访问本服务数据库或外部设施。目标导入方向为 inbound → application → domain；outbound adapters → application ports/domain primitives；cmd 负责装配。domain 不导入 Gin、GORM、MQTT、MediaMTX 或其他基础设施 SDK。禁止服务间 Go 包导入、跨 schema 查询和共享可变领域模型；服务协作使用有版本的 HTTP/MQTT/outbox 合同，数据由所属服务写入。

每个现有 Go module 是独立服务的边界，目标目录建议为 cmd/service-name、internal/domain、internal/application、internal/transport/{http,mqtt,worker}、internal/adapters/outbound/{postgres,serviceclient,broker,media,push,filesystem}。目录示例不要求一次迁移，也不能仅靠目录命名宣称依赖规则已满足。

Web 目标沿用单一 Vue 工程：页面和布局负责呈现与导航，按业务 feature 放置状态及用例，类型化 API client 负责 HTTP 边界，共享 components 只放通用展示；服务端才是身份、工作空间和能力授权权威。Atlas 目标由 main.cpp 组装 agent/runtime 用例，硬件、MQTT、RTSP、SQLite 等适配器实现稳定端口，算法插件依稳定接口接入；现有 subsystem 目录不自动等同于已具备领域分层。

采用逐服务、逐垂直切片迁移；每次保留既有 API、数据库所有权、设备协议和媒体链路，先抽用例/端口，再替换适配器并验证行为。依赖检查需阻止 domain 导入框架、服务间包依赖、循环依赖和越过所有权的数据库访问。完整目标和代码级验收仍处于待实现状态。
