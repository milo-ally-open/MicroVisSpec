# device-service 实现任务书

> 状态：首发实现任务书；以[产品需求](../Analysis/PRD.md)、[架构设计](../Design/STRUCTURE.md)和[总技术规格](../Analysis/SPEC.md)为准。
>
> 责任边界：device-service 拥有设备、摄像头、算法能力、期望配置与运行实报；event-service 查询其归属，media-service 查询摄像头归属并独立保存流来源。
>
> 首发边界：RK3588 设备由账号用户手动登记，经一次性票据激活；业务状态、事件和配置使用 MQTT。SSH 只供人工运维，不参与业务通信。

## 一、任务目标

交付可管理当前账号边缘设备的 Go/Gin/GORM 服务，并让 RK3588 通过受控 MQTT 身份上报状态、接收受支持的算法参数命令。设备在线、算法运行和视频可播放须分别呈现，不能以其中一个状态代替另外两个。

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
2. 摄像头由设备侧稳定 `camera_key` 对应；同一设备内重复登记处理为明确冲突或幂等返回，不制造两个 camera_id。媒体源地址由 media-service 管理，本服务不保存 RTSP 密码。
3. 算法展示以边缘报告的算法键、版本、运行状态与参数能力为准，不在服务端假定某一业务算法永远存在。位置修改只影响当前资产资料，不覆盖 event-service 已保存的历史事件快照。
4. 停用设备时，在同一事务中将设备标为不可用并写入 `device.disabled` outbox，负载仅含 `account_id`、`device_id`；异步投递 media-service 终止关联流与租约，失败持续重试。同时撤销 broker 凭据并定期对账；event-service 的设备归属查询立即拒绝停用设备的后续事件。历史事件按 event-service 留存规则继续可查，重复停用不能复活旧凭据。
5. 基础统计只聚合当前账号的有效登记设备，并用心跳规则计算 `online`；前端分析页从本服务取得设备卡片，不建立独立统计服务。

### 3.3 交付物与阶段验收

- 交付资产 API、分页/筛选和数据归属说明、设备与摄像头迁移。
- 账号 A 无法查询、修改或停用账号 B 的设备与摄像头；登记设备在未激活和未收到心跳时不显示在线。
- 修改设备位置后，已发生事件的地点快照不改变；停用设备不抹除历史事件。

---

## 四、第三阶段：RK3588 激活、MQTT 身份和 ACL

### 4.1 激活接口

| 方法与路径 | 任务与返回 |
| --- | --- |
| `POST /api/v1/devices/{deviceId}/activation-tickets` | 当前账号签发一次性票据，返回只显示一次的凭据及 10 分钟到期时间；重签使旧票据失效。 |
| `POST /edge/v1/activate` | 边缘设备提交 `device_id` 和票据；成功后取得独立 MQTT `client_id`、设备密码与 broker 接入信息。 |

### 4.2 实现任务

1. 票据用足够随机的值，数据库只存哈希；按设备绑定、10 分钟有效、一次消费，并对签发和兑换限流。无效、过期、重复兑换或已停用设备不能取得 broker 身份。
2. 为每台 RK3588 分配独立 MQTT client_id 和密码，使用 Mosquitto 2.x Dynamic Security 管理身份与 ACL；设备密码仅首次激活返回。轮换/停用设备身份时同步撤销 broker 权限，不依靠设备 IP 做可信判断。
3. MQTT 使用 TLS、3.1.1、固定 client_id 的持久会话。设备仅可发布自己的 `devices/{deviceId}/state`、`events`、`command-acks`，并仅可订阅自己的 `event-acks`、`commands`；后台服务使用独立的最小权限身份。
4. 对 broker 开启持久化并限制连接、报文和离线队列大小。设备断网时本地保留未收到业务 ACK 的事件，重连仍用同一 event_id 重发；本服务只负责设备接入合同，不代替 event-service 的事件去重与 ACK。

### 4.3 交付物与阶段验收

- 交付激活 API、票据状态模型、broker Dynamic Security 配置/恢复步骤及 RK3588 接入说明。
- 有效票据只能兑换一次；重新签发使旧票据失效；设备 A 无法读写设备 B 的主题。
- 停用后旧 MQTT 身份无法继续发布状态/事件或接收命令；重复停用和激活请求不会产生多个有效身份。

---

## 五、第四阶段：心跳、实报状态和参数命令

### 5.1 消息合同

| 主题 | device-service 职责 |
| --- | --- |
| `devices/{deviceId}/state` | 消费心跳、摄像头与算法实报，并更新最后有效心跳。 |
| `devices/{deviceId}/commands` | 发布带 `command_id` 和期望配置版本的参数命令。 |
| `devices/{deviceId}/command-acks` | 消费命令执行结果、实报版本与失败原因。 |

所有消息遵守总 SPEC 的 `schema_version=1`、`device_id`、`sent_at` 和主题/凭据归属校验；不信任 payload 中自报的 `account_id`。

### 5.2 浏览器 API 与实现任务

`PUT /api/v1/devices/{deviceId}/algorithms/{assignmentId}/parameters` 接收 `expected_version`、`parameters`，返回新的期望版本与 `pending` 状态。

1. 设备每 30 秒发送状态心跳；超过 90 秒无有效心跳判离线。在线状态仅表示设备代理可联系，不代表算法正常或 RTSP 可播放。
2. 设备实报含算法键、版本、运行状态、受支持参数 schema、当前实报参数与实报配置版本。用户只能修改能力描述明确允许的参数；未知或超范围值拒绝，不向设备发送不受支持命令。
3. 参数修改采用 `expected_version` 乐观并发：保存新期望版本和命令记录，再以 QoS 1 发布。重复发布保持同一 `command_id`；设备重复处理命令后回传相同业务结果。
4. 收到对应 ACK 与实报版本后才标记 `applied`；失败、超时、设备离线或版本不符保留 `pending`/`failed` 状态和可解释原因，UI 不宣称配置已生效。迟到 ACK 必须核对命令与期望版本，不能覆盖更新版本的结果。
5. 发布失败通过持久命令记录重试或对账，服务恢复后继续处理；不因 MQTT QoS 1 投递成功就认定边缘执行成功。

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

- 源码、迁移、镜像、外部/内部接口合同、MQTT 主题及 ACL 配置、RK3588 激活与状态联调说明。
- 可复现记录覆盖 A/B 账号隔离、票据重用与过期、设备主题越权、90 秒离线、参数成功/失败/乱序回执、停用撤权及统计账号隔离。
- 本服务只处理设备控制面；截图、事件处置和播放流由对应服务管理，SSH 不承担事件或命令链路。

## 七、变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-04 | 从总技术规格拆分 device-service 阶段任务、边缘接入合同与验收。 |
