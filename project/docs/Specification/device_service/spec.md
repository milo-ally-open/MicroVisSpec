# device-service 实现任务书

> 状态：首发实现任务书；以[产品需求](../../Analysis/PRD.md)、[架构设计](../../Design/STRUCTURE.md)和[总技术规格](../../Analysis/SPEC.md)为准。
>
> 责任边界：device-service 拥有设备、摄像头、算法能力、期望配置与运行实报；event-service 查询其归属，media-service 查询摄像头归属并独立保存流来源。
>
> 首发边界：设备由账号用户登记；首次部署由管理员预置 Atlas agent 的 HTTPS Origin、CA 和可信 MQTT 身份。设备上线后，账号用户仅在网页发起激活/续期；agent 自动领取 MQTT 凭据，用户不需要在 Atlas 终端运行命令。业务状态、事件和配置使用 MQTT。

## 稳定需求与验收索引

> 本索引用于跨文档追踪；详细行为以本文件正文为准。编号保持稳定，需求变更时同步更新。

### REQ-01 资产归属

设备和摄像头按账号隔离。

- **AC-01.1**：读写校验 account_id 和父子关系。
- **AC-01.2**：停用设备禁止业务操作且保留历史追溯。

### REQ-02 激活与设备身份

设备激活和 MQTT 身份具备时效及设备级隔离。

- **AC-02.1**：激活票据有时效且只能消费一次。
- **AC-02.2**：设备凭据不能访问其他设备主题。
- **AC-02.3**：激活响应中的 MQTT broker 地址来自独立的边缘可达配置；不得返回服务容器内部的 broker DNS 地址；设备使用该地址通过 TLS 接入。
- **AC-02.4**：激活响应中的 MQTT `client_id` 与 Mosquitto Dynamic Security 为该设备登记的固定 client ID 完全一致；端侧连接合同使用该原值。
- **AC-02.5**：签发票据时 `duration_seconds` 为 `null` 表示无限期，正整数表示自激活成功时起的授权秒数；缺省字段兼容旧客户端并按无限期处理，超出支持范围的值拒绝。
- **AC-02.6**：激活期限到达后，服务在最多 15 秒调度间隔内将设备置为 `active=false`、`activation_state=expired`，写入 `device.disabled` outbox 并立即尝试撤销 MQTT 权限；撤销失败由既有对账循环重试。已过期设备的事件、状态、媒体和设备业务认证均拒绝。
- **AC-02.7**：账号所有者仍可在列表/详情中查看 `expired` 设备并在网页提交新租期请求；agent 领取后使用新 MQTT 密码恢复设备，按新请求期限覆盖到期时间，`null` 清除到期时间，并通过 `device.activated` 通知恢复已保存媒体来源。手动 `disabled` 设备不允许通过此流程恢复。
- **AC-02.8**：已预置可信设备身份的所有者可在网页提交 `duration_seconds` 并查询激活请求状态；网页响应不含激活票据或 MQTT 凭据。Atlas agent 使用现有设备凭据通过 TLS 轮询领取请求，成功后服务端轮换 MQTT 身份并将期限写入设备记录；页面在设备确认保存新凭据前显示 `syncing`。
- **AC-02.9**：Atlas agent 在激活请求期间或已过期时仍可使用原设备身份访问唯一的 HTTPS 激活轮询路由；该例外只可读取并领取本设备待处理激活请求，不恢复旧 MQTT ACL，也不开放设备业务认证。已提交但未确认的新凭据可由旧 HTTPS 身份幂等重领；收到新密码后，agent 下一次轮询确认并使旧身份失效。手动停用设备不可轮询续期。

### REQ-03 状态与配置

在线、算法运行、播放状态和配置实报分别表达。

- **AC-03.1**：心跳过期可判离线，各状态不互相替代。
- **AC-03.2**：匹配期望版本的设备 ACK 才表示配置已应用。
- **AC-03.3**：消费 Atlas agent 的 `schema_version`、`device_id`、`sent_at` 及设备/摄像头/NPU/模型/流/MQTT、配置版本、队列状态字段；这些字段经严格类型与范围校验后与摄像头/算法数组一起原子保存，未知字段拒绝，只有有效状态才刷新 `last_seen_at`。

### REQ-04 内部查询与恢复

跨服务归属查询不破坏数据所有权且故障可识别。

- **AC-04.1**：内部接口验证调用方并最小化返回。
- **AC-04.2**：依赖故障、重连和停用状态可观测。

### REQ-05 设备工作区的视频来源配置入口

设备详情工作区应支持为已登记摄像头配置可达的 RTSP 地址；媒体来源数据与 MediaMTX 配置仍归 media-service 所有。

- **AC-05.1**：摄像头所有者可从设备详情打开该摄像头的视频来源配置；页面只提供 RTSP 地址字段并调用 media-service，不在 device-service 中复制来源配置或建立第二份持久化。
- **AC-05.2**：配置请求仍经 media-service 核对 device-service 中的摄像头归属；其他账号的摄像头不可读取或更新来源地址。

### REQ-06 网页 SSH 引导首次设备接入

设备所有者可在网页使用管理员提供的板端 SSH 账号完成 agent 首次部署和可信激活，不需要复制激活票据或登录 Atlas 终端。

- **AC-06.1**：接入接口仅使用当前账号拥有设备记录中的私网 IP 和 TCP/22；不接受请求方传入主机地址、端口或远端命令。目标 IP 必须匹配部署配置的 Atlas SSH CIDR allowlist。
- **AC-06.2**：首次 SSH 连接先显示主机公钥 SHA-256 指纹，用户确认后才发送密码；服务端仅接受该次确认指纹，后续连接必须匹配设备已保存指纹。密钥变化时拒绝连接并提示重新核验。
- **AC-06.3**：SSH 用户名与密码只存在于单次 HTTPS 请求和执行进程内；不得写入设备资料、任务记录、文件、命令行参数、响应或日志。任务状态仅保存阶段、时间、安全错误码及完成状态。
- **AC-06.4**：无 agent 时使用镜像内的仓库 agent-only 源码包在目标板构建并安装 agent；保留已有 `/etc/yunshao` 环境配置和业务 JSON，校验 CANN 环境后才继续。CA 使用部署机提供的 API TLS 与 MQTT CA，临时文件使用受限权限并在成功/失败后清理。
- **AC-06.5**：服务端为当前设备及所选授权期限签发一次性票据，由固定 agent 命令经 SSH 标准输入兑换；票据与 MQTT 凭据不返回浏览器。Agent 成功启动后，设备进入既有 MQTT 心跳与状态上报链路。
- **AC-06.6**：接入任务异步报告安全阶段；只允许同一设备一个未完成任务。任务查询按 `account_id` 校验。网络/认证/指纹/缺依赖/构建/激活错误可区分；所有失败均可安全重试，远端临时文件清理。

## 一、任务目标

交付可管理当前账号边缘设备的 Go/Gin/GORM 服务，并让 Atlas 200I DK A2 通过受控 MQTT 身份上报状态、接收受支持的算法参数命令。设备在线、算法运行和视频可播放须分别呈现，不能以其中一个状态代替另外两个。

阶段顺序：服务基础 → 资产登记 → 激活与 MQTT 权限 → 状态及算法参数 → 跨服务查询和交付。

---

## 二、第一阶段：服务基础与数据所有权

### 2.1 实现任务

1. 在 `project/src/services/device-service` 下建立 Go 模块、Gin 入口、配置、数据库迁移和可构建 Dockerfile。提供内部 `/health/live`、`/health/ready`，就绪状态检查 PostgreSQL 与必需的 MQTT 管理依赖。
2. 在独占的 PostgreSQL `device` schema 中建立 `devices`、`cameras`、`algorithm_assignments`、`activation_tickets`、`outbox` 以及命令/回执记录。设备和摄像头使用 UUID；设备按 `(account_id,id)` 查询，`camera_key` 在设备内唯一；激活票据只保存哈希与失效时间。
3. 设备记录包含登记的名称、IP、位置、激活状态、最后心跳和实报状态；登记 IP 是资产属性，不作为认证依据。边缘凭据、broker 管理凭据通过部署机密注入。
4. 对外错误、时间、分页、账号身份和内部调用遵守总 SPEC。不得直接读写 auth、event、media 或 push 的数据库 schema。

### 2.2 交付物与阶段验收

- 交付服务容器、迁移、配置说明和健康接口。
- 空库可迁移并重启保持资产记录；服务无法履行登记/激活关键依赖时就绪状态如实显示。
- 服务数据库账号不能修改其他服务 schema；日志不包含激活票据或 MQTT 明文密码。

---

## 三、第二阶段：设备、摄像头和算法资产

### 3.1 浏览器 API

| 方法与路径 | 任务与返回 |
| --- | --- |
| `POST /api/v1/devices` | 接收 `name`、`ip`、`location`；登记为 `pending`，返回 `device_id`。 |
| `GET /api/v1/devices`、`GET /api/v1/devices/{deviceId}` | 分页查询当前账号设备及摄像头、算法、状态与配置。 |
| `PATCH /api/v1/devices/{deviceId}` | 修改名称、IP 和位置；不修改归属账号。 |
| `DELETE /api/v1/devices/{deviceId}` | 停用设备并从当前设备列表移除。 |
| `POST /api/v1/devices/{deviceId}/cameras` | 接收 `name`、`camera_key`；返回 `camera_id`。 |
| `GET /api/v1/devices/{deviceId}/algorithms` | 返回算法名称、版本、运行状态、能力描述和实报参数。 |
| `GET /api/v1/devices/stats` | 返回本账号 `registered`、`online` 和 `as_of`。 |

### 3.2 实现任务

1. 设备由当前可信 `account_id` 拥有；所有列表、详情、修改、停用、摄像头和算法查询都以账号过滤。请求体携带的 `account_id` 一律不作为归属依据。
2. 摄像头由设备侧稳定 `camera_key` 对应；同一设备内重复登记处理为明确冲突或幂等返回，不制造两个 camera_id。设备详情可作为 RTSP 来源配置入口，但媒体源地址由 media-service 管理，本服务不存储或复制来源配置。
3. 算法展示以边缘报告的算法键、版本、运行状态与参数能力为准，不在服务端假定某一业务算法永远存在。位置修改只影响当前资产资料，不覆盖 event-service 已保存的历史事件快照。
4. 停用设备时，在同一事务中将设备标为不可用并写入 `device.disabled` outbox，负载仅含 `account_id`、`device_id`；异步投递 media-service 终止关联流与租约，失败持续重试。同时撤销 broker 凭据并定期对账；event-service 的设备归属查询立即拒绝停用设备的后续事件。历史事件按 event-service 留存规则继续可查，重复停用不能复活旧凭据。
5. 基础统计只聚合当前账号的有效登记设备，并用心跳规则计算 `online`；前端分析页从本服务取得设备卡片，不建立独立统计服务。

### 3.3 交付物与阶段验收

- 交付资产 API、分页/筛选和数据归属说明、设备与摄像头迁移。
- 账号 A 无法查询、修改或停用账号 B 的设备与摄像头；登记设备在未激活和未收到心跳时不显示在线。
- 修改设备位置后，已发生事件的地点快照不改变；停用设备不抹除历史事件。

---

## 四、第三阶段：Atlas 200I DK A2 激活、MQTT 身份和 ACL

### 4.1 激活接口

| 方法与路径 | 任务与返回 |
| --- | --- |
| `POST /api/v1/devices/{deviceId}/activation-tickets` | 当前账号提交 `{"duration_seconds": null}`（无限期）或正整数秒数（自定义期限），签发只显示一次的票据及 10 分钟票据到期时间；重签使旧票据失效。缺省 `duration_seconds` 兼容旧客户端并按无限期处理。 |
| `POST /api/v1/devices/{deviceId}/activation-requests` | 当前账号提交 `{"duration_seconds": null}` 或正整数秒数；返回不含秘密值的请求 ID、状态和 10 分钟等待期限。只有已预置可信设备身份的设备可通过网页流程激活。 |
| `GET /api/v1/devices/{deviceId}/activation-requests/{requestId}` | 当前账号查询 `pending`、`syncing`、`activated`、`expired` 或 `superseded` 状态；服务端已激活但设备尚未确认保存新凭据时返回 `syncing`；成功时附服务器确认的 `activation_expires_at`。 |
| `POST /edge/v1/activate` | 边缘设备提交 `device_id` 和票据；成功后取得独立 MQTT `client_id`、设备密码、broker 接入信息及 `activation_expires_at`（无限期时为 `null`）。期限从成功兑换时开始。端侧必须原样使用返回的 `client_id`。`mqtt.broker_url` 必须取自 `MQTT_ADVERTISED_BROKER_URL`，不能泄露/复用服务容器内部的 `MQTT_BROKER_URL`。 |
| `GET /edge/v1/devices/{deviceId}/activation` | Atlas agent 使用设备 MQTT username/password 的 HTTPS Basic 身份轮询。存在待处理网页请求时返回新的 MQTT 凭据及租期；响应丢失时旧身份可重领同一凭据，agent 用新身份再次轮询后确认并清除旧身份；已激活且无待处理请求时仅返回租期同步状态；无请求的过期设备返回 `204`。该路由不得记录 Authorization。 |

| `POST /api/v1/devices/{deviceId}/provisioning/ssh-host-key` | 当前账号获取登记私网设备 TCP/22 的 SSH 主机指纹；无账号密码认证，也不接受目标地址。返回设备已固定指纹或待首次确认的指纹。 |
| `POST /api/v1/devices/{deviceId}/provisioning` | 当前账号提交 SSH 用户名/密码、已确认的主机指纹和 `duration_seconds`；密码仅用于此请求。服务在后台执行 agent-only 安装和票据激活，返回不含秘密的 `job_id` 与状态。 |
| `GET /api/v1/devices/{deviceId}/provisioning` | 当前账号读取该设备最近一次接入任务；无任务时返回空值，使页面刷新或重新进入后能恢复任务进度。 |
| `GET /api/v1/devices/{deviceId}/provisioning/{jobId}` | 返回账号范围内的 `queued`、阶段、`succeeded` 或 `failed` 状态及安全错误代码；不返回远端原始输出或任何凭据。 |

### 4.2 实现任务

1. 票据用足够随机的值，数据库只存哈希；按设备绑定、10 分钟有效、一次消费，并对签发和兑换限流。无效、过期、重复兑换或已停用设备不能取得 broker 身份。
2. 为每台 Atlas 200I DK A2 分配独立 MQTT client_id 和密码，使用 Mosquitto 2.x Dynamic Security 管理身份与 ACL；设备密码仅首次激活返回。轮换/停用设备身份时同步撤销 broker 权限，不依靠设备 IP 做可信判断。
3. `MQTT_BROKER_URL` 是服务容器访问 broker 的 TLS URL；`MQTT_ADVERTISED_BROKER_URL` 是激活响应下发给边缘设备的 TLS URL。两项分别配置且均须为 `ssl://` URL；advertised 地址应解析/路由到运行 broker TLS listener 的部署端点。配置缺失或不合法时 device-service 启动失败，不得回退到内部地址或静默使用示例地址。
4. MQTT 使用 TLS、3.1.1、固定 client_id 的持久会话。设备仅可发布自己的 `devices/{deviceId}/state`、`events`、`command-acks`，并仅可订阅自己的 `event-acks`、`commands`；后台服务使用独立的最小权限身份。
5. 对 broker 开启持久化并限制连接、报文和离线队列大小。设备断网时本地保留未收到业务 ACK 的事件，重连仍用同一 event_id 重发；本服务只负责设备接入合同，不代替 event-service 的事件去重与 ACK。

### 4.3 交付物与阶段验收

- 交付激活 API、票据状态模型、broker Dynamic Security 配置/恢复步骤及 Atlas 200I DK A2 接入说明。
- 有效票据只能兑换一次；重新签发使旧票据失效；设备 A 无法读写设备 B 的主题。
- 激活响应返回 configured advertised broker URL；检查该值与 `MQTT_BROKER_URL` 解耦，并由边缘设备经 TLS 实际连通 advertised 地址。
- 激活响应的 `client_id` 与 Dynamic Security 固定 client ID 相同，Atlas 使用该值完成 MQTT CONNECT 并成功通过 broker ACL。
- 停用后旧 MQTT 身份无法继续发布状态/事件或接收命令；重复停用和激活请求不会产生多个有效身份。

---

## 五、第四阶段：心跳、实报状态和参数命令

### 5.1 消息合同

| 主题 | device-service 职责 |
| --- | --- |
| `devices/{deviceId}/state` | 消费心跳、摄像头与算法实报，并更新最后有效心跳。 |
| `devices/{deviceId}/commands` | 发布带 `command_id` 和期望配置版本的参数命令。 |
| `devices/{deviceId}/command-acks` | 消费命令执行结果、实报版本与失败原因。 |

所有消息遵守总 SPEC 的 `schema_version=1`、`device_id`、`sent_at` 和主题/凭据归属校验；不信任 payload 中自报的 `account_id`。Atlas agent 状态可在 `cameras`/`algorithms` 数组外提供 `device_online`、`camera_state`、`npu_state`、`model_catalog`、`model_state`、`stream_state`、`mqtt_state`、`desired_config_version`、`applied_config_version`、`queue_depth` 和 `outbox`。device-service 必须识别这些字段并将完整有效报告写入 `reported_state`；不能因为端侧提供上述额外合同字段而把整条心跳拒绝。字段缺失的旧版设备仍可只发送数组，但未知字段、错误类型、负版本或负队列深度必须拒绝。

### 5.2 浏览器 API 与实现任务

`PUT /api/v1/devices/{deviceId}/algorithms/{assignmentId}/parameters` 接收 `expected_version`、`parameters`，返回新的期望版本与 `pending` 状态。

1. 设备每 30 秒发送状态心跳；超过 90 秒无有效心跳判离线。在线状态仅表示设备代理可联系，不代表算法正常或 RTSP 可播放。
2. 设备实报含算法键、版本、运行状态、受支持参数 schema、当前实报参数与实报配置版本。用户只能修改能力描述明确允许的参数；未知或超范围值拒绝，不向设备发送不受支持命令。
3. 参数修改采用 `expected_version` 乐观并发：保存新期望版本和命令记录，再以 QoS 1 发布。重复发布保持同一 `command_id`；设备重复处理命令后回传相同业务结果。
4. 收到对应 ACK 与实报版本后才标记 `applied`；失败、超时、设备离线或版本不符保留 `pending`/`failed` 状态和可解释原因，UI 不宣称配置已生效。迟到 ACK 必须核对命令与期望版本，不能覆盖更新版本的结果。
5. 发布失败通过持久命令记录重试或对账，服务恢复后继续处理；不因 MQTT QoS 1 投递成功就认定边缘执行成功。

### 5.2.1 首发课堂告警规则配置

`classroom_behavior_detection` 的 `parameters.classroom_rules` 包含固定类别 `hand-raising`、`reading`、`writing`。新 assignment 和完整配置快照必须将三类的 `enabled` 初始化为 `false`。每类启用需显式传入 `confidence_threshold`（0–1）、`minimum_duration_ms`（正整数）、`cooldown_ms`（非负整数）和 `severity`（`low|medium|high|critical`）；缺失字段、类别未知或值越界时整个配置拒绝，不改变已生效版本。保存成功只代表产生新的期望版本，设备 ACK 前状态仍为 `pending`。

配置版本须保留不可变快照及设备实际应用状态/时间，以供 event-service 校验延迟上报的规则事件。新增仅内网接口 `POST /internal/v1/devices/{deviceId}/classroom-events/validate`：服务凭据验证后接收 `camera_id`、`algorithm_version`、`rule_config_version`、`behavior_class`、`confidence`、`observed_duration_ms`、`severity`、`occurred_at`。有效请求返回 `200`、`allowed=true`、可信 `account_id`、设备/摄像头名称与地点快照及校验的配置版本。设备/摄像头不存在或已停用返回 `404`；规则禁用、未应用、越阈/时长/等级不符返回 `422` 与稳定 reason_code；服务凭据错误返回 `401/403`；数据库或历史版本暂时不可用返回 `503`。event-service 仅对 `allowed=true` 继续入库；422 生成终结 `rejected` 应用 ACK，503 不终结 ACK 并允许边缘重试。边缘不自报可信 `account_id`。

设备通过 HTTPS 向 event-service 上传截图时，以激活时签发的 MQTT username/password 作为 HTTP Basic 设备凭据。event-service 不持有设备密码库，而是以内网服务凭据调用 `POST /internal/v1/devices/{deviceId}/http-authenticate`，请求只含 Basic username/password；device-service 校验设备仍启用、credential 未撤销及密码哈希，返回最小的 `allowed`、可信 `device_id` 结果。停用设备立即使该验证失败。内部接口不得返回凭据、哈希或 MQTT 其他字段；认证失败与数据库暂不可用分别返回 401 和 503。日志不得记录 Basic Authorization 或请求体。

### 5.3 交付物与阶段验收

- 交付 MQTT 消息 schema、参数能力和命令状态 API、边缘代理交互示例。
- 停止心跳超过 90 秒后设备显示离线；算法停止和视频断流分别显示，不混为设备离线。
- 两个同时修改请求产生可识别的 409 冲突；成功 ACK 后才显示已生效，失败/超时和旧 ACK 均不能伪造成功。

---

## 六、第五阶段：内部查询、运行和整链路交付

### 6.1 实现任务

1. 提供仅限服务网络和服务凭据访问的设备/摄像头归属查询：event-service 按设备与摄像头核对并取得发生时名称、位置及算法快照；media-service 按 camera_id 核对所属账号及是否停用。内部接口返回稳定 ID 与必要字段，不提供跨 schema 数据库访问。
2. 对内查询设置超时与错误处理：归属不可验证时 event-service 不确认业务 ACK，media-service 不发放新的播放授权；恢复后通过重试或对账继续处理。
3. 记录设备最后心跳、离线数、激活失败、broker 凭据同步失败、命令待确认/失败数和 MQTT 积压。审计激活、撤销与参数修改；日志不记录设备密码和票据。
4. 提供 broker 持久化、安全配置与数据库备份/恢复步骤；恢复后核对数据库设备状态和 broker 中实际身份/ACL，重投未送达的 `device.disabled`，避免已停用设备重新获得权限或媒体流继续开放。

### 6.2 最终交付物与验收

- 源码、迁移、镜像、外部/内部接口合同、MQTT 主题及 ACL 配置、Atlas 200I DK A2 激活与状态联调说明。
- 可复现记录覆盖 A/B 账号隔离、票据重用与过期、设备主题越权、90 秒离线、参数成功/失败/乱序回执、停用撤权及统计账号隔离。
- 本服务只处理设备控制面；截图、事件处置和播放流由对应服务管理，SSH 不承担事件或命令链路。

## 七、变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-21 | 在设备详情工作区增加单字段 RTSP 配置入口的产品合同；摄像头归属仍由本服务提供，来源 URL 与媒体状态由 media-service 持有。 | 用户要求从系统配置边缘设备并精简来源配置。 |
| 2026-10-23 | 增加无限期/自定义期限设备授权、到期停用撤权及过期设备续期合同。 | 用户要求用户可选择设备使用期限，并确认到期自动停用及撤销 MQTT 权限。 |
| 2026-10-04 | 从总技术规格拆分 device-service 阶段任务、边缘接入合同与验收。 |
