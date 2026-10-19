# media-service 实现任务书

> 状态：首发实现任务书；以[总技术规格](../../Analysis/SPEC.md)、[架构设计](../../Design/STRUCTURE.md)和[产品需求](../../Analysis/PRD.md)为准。
> 责任边界：media-service 管理流配置、观看权限、配额与 MediaMTX 对账；MediaMTX 拉取和分发媒体；device-service 维护设备、摄像头及账号归属。
> 首发边界：桌面实时监控大屏最多 12 路活动源流、两个完整 12 分屏会话；手机 PWA 不提供监控大屏。不做录像回放或后端统一转码。

## 稳定需求与验收索引

> 本索引用于跨文档追踪；详细行为以本文件正文为准。编号保持稳定，需求变更时同步更新。

### REQ-01 来源配置与对账

仅配置已授权摄像头来源，控制面重启后恢复期望配置。

- **AC-01.1**：校验摄像头归属并加密来源凭据。
- **AC-01.2**：MediaMTX 重启或漂移后重新对账。

### REQ-02 播放授权与配额

播放会话经过授权并受并发上限约束。

- **AC-02.1**：未授权账号无法获得有效播放入口。
- **AC-02.2**：12 路活动源流/24 个连接目标以实测证据确认。

### REQ-03 浏览器播放

播放器按支持协议回退并准确呈现不可用状态。

- **AC-03.1**：WebRTC/HLS 选择和回退遵循规格。
- **AC-03.2**：不承诺超出编码、浏览器及服务边界的能力。

### REQ-04 租约与恢复

租约到期释放配额，运行故障可诊断和恢复。

- **AC-04.1**：关闭、超时或撤销后回收租约及播放权限。
- **AC-04.2**：健康、指标和重启恢复暴露依赖异常。

## 一、任务目标

在 `project/src/services/media-service/` 实现独立的 Go/Gin/GORM 控制面，并将 MediaMTX 原生可执行文件与 `mediamtx.yml` 配置保存在同一目录；不将 MediaMTX 打包为自定义 Docker 镜像或 Compose 容器，也不增加包装启动器。运维直接使用该二进制和配置文件启动；本地含凭据配置文件由 Git 忽略，仓库只提供无敏感值的示例配置。Atlas 200I DK A2 处理后的 RTSP 流经 MediaMTX 向桌面浏览器提供 WebRTC，失败时回退 HTTPS HLS。完整链路为：

**摄像头归属 → 流来源登记 → MediaMTX 配置/拉流 → 账号授权与观看配额 → WebRTC/HLS 播放 → 状态与故障反馈**。

任务按五个阶段实施。首发由 Atlas 200I DK A2 侧生成兼容浏览器的码流；本服务不承载视频帧、不对视频软件转码。流路径和来源凭据均不得直接暴露给未授权的浏览器。[MediaMTX 文档](https://mediamtx.org/docs/read/web-browsers)

## 二、第一阶段：服务骨架与数据所有权

### 2.1 实现任务

1. 在现有 `go/` 占位目录内建立 Go module、入口、配置、健康接口和数据库迁移；实现后把占位 Dockerfile 补为可构建镜像。`/health/live` 表示进程存活，`/health/ready` 反映 PostgreSQL 与必需的 MediaMTX 控制面状态。
2. 由 media-service 独占 PostgreSQL `media` schema，至少保存 `streams`、`viewer_leases` 与 `inbox`。`streams` 保存 `stream_id`、`account_id`、经 device-service 核对的 `device_id`、唯一 `camera_id`、加密的 RTSP URL/凭据、配置版本及状态；`viewer_leases` 保存 `session_id`、流 ID、到期时间和播放器关联；`inbox` 对 `session.revoked`、`device.disabled` 去重。
3. 来源凭据使用部署机密加密保存，密钥不写仓库、镜像或普通日志。数据库账号仅有 `media` schema 所需权限；不直接读取 device-service 的表。
4. 日志和指标携带请求 ID、camera_id、stream_id；不输出 RTSP 明文 URL、来源密码、浏览器 Cookie、内部服务凭据或可复用播放凭据。

### 2.2 交付物与阶段验收

- 可构建的独立服务镜像、media schema 迁移与配置说明。
- 空库启动完成迁移；重启后流配置和租约恢复/清理符合预期。
- media-service 无法直接读写 device、event、auth 等其他 schema；关键依赖不可用时就绪状态和日志能够指出原因。

---

## 三、第二阶段：摄像头来源配置与 MediaMTX 对账

### 3.1 接口合同

| 接口 | 实现要求 |
| --- | --- |
| `PUT /api/v1/media/cameras/{cameraId}/source` | 接收 RTSP URL 和独立的 `source_username`、`source_password`；先向 device-service 核对摄像头属于当前账号，再保存来源并返回 `stream_id`、`config_status`。 |
| `GET /api/v1/media/cameras/{cameraId}/status` | 返回设备关联、来源可达、拉流状态、最近错误和浏览器编码兼容提示；不能仅复制设备在线状态。 |
| MediaMTX 内部控制 API | 仅 media-service 调用，用于按 `stream_id` 新增、修改、删除路径并读取实际状态；不经公网暴露。 |

### 3.2 实现任务

1. `camera_id` 必须是 device-service 已登记且属于当前账号的摄像头；浏览器请求体中的 `account_id` 无效。RTSP 用户名/密码单独传递，拒绝在 URL 中嵌入凭据；数据库保存加密值。
2. MediaMTX 路径使用不暴露设备信息的 `stream_id`。将来源设置为边缘设备输出的私有 RTSP 地址，显式启用 `sourceOnDemand`；来源无人观看时释放拉流，恢复观看时向界面呈现连接中状态。[MediaMTX 配置](https://mediamtx.org/docs/references/configuration-file/)
3. 维护“期望配置版本”和“MediaMTX 实际配置版本”。配置更新失败时标记 `pending`/`failed`，后台定期对账重试；不得把数据库已保存误认为 MediaMTX 已生效。
4. 接收 device-service 的 `device.disabled`，按设备关闭关联流路径和观看租约；重复通知幂等。定期向 device-service 对账摄像头归属与设备启用状态，补偿消息延迟或 MediaMTX 重启。历史事件、截图与处置由 event-service 按自己的留存规则管理。
5. 监控“设备在线”“RTSP 来源可达”“媒体正在拉取”“浏览器可播放”四个不同状态，界面按具体原因提示。

### 3.3 交付物与阶段验收

- 摄像头来源配置和状态接口、MediaMTX 配置对账任务、凭据保存与轮换说明。
- MediaMTX 重启后，路径可由 media-service 自动恢复；来源密码不会出现在浏览器响应、控制面日志或数据库明文字段。
- 不存在的摄像头、其他账号摄像头和不合法 RTSP 来源均被拒绝；摄像头删除后不再允许观看。

---

## 四、第三阶段：播放授权与并发配额

### 4.1 浏览器与 Nginx 合同

| 接口或入口 | 实现要求 |
| --- | --- |
| `GET /api/v1/media/cameras/{cameraId}/playback` | 账号校验后返回受保护的 `whep_url`、`hls_url`、`stream_id`、当前状态和观看租约；不返回原始 RTSP 地址或凭据。 |
| `POST /api/v1/media/viewer-leases/{leaseId}/renew` | 播放期间每 15 秒续租，服务端租约有效期 30 秒。 |
| `DELETE /api/v1/media/viewer-leases/{leaseId}` | 页面关闭、切换流或主动停止播放时释放观看连接；重复释放成功。 |
| `/media/{streamId}/whep`、`/media/{streamId}/index.m3u8` | Nginx 代理到 MediaMTX；WHEP 信令、HLS 播放列表及每个分片都经过账号与摄像头归属鉴权。 |
| `GET /internal/v1/media/authorize-playback` | 仅 Nginx 的内部授权子请求可调用；读取经 auth-service 校验后覆写的 `X-Account-ID`、`X-Session-ID` 和原始 `X-Original-URI`，不接收请求体。URI 必须是 `/media/{streamId}/...`，可带 `lease_id` 查询参数；每个 HLS 子资源仍按当前会话、流和有效租约检查。 |

### 4.2 实现任务

1. Nginx 对媒体请求先调用 auth-service 会话/Origin/CSRF 授权子请求，再将已验证会话身份及原始媒体 URI 交给 media-service 的 `GET /internal/v1/media/authorize-playback`；该入口验证账号、stream_id、会话和有效观看租约。Nginx 清除客户端伪造的身份头；MediaMTX 直接 HTTP 读入口仅允许经受控代理访问，并使用内部身份，不能保留默认匿名可读配置。[MediaMTX 鉴权](https://mediamtx.org/docs/features/authentication)
2. 全平台最多同时活动 12 路来源、24 个观看连接；一个完整 12 分屏桌面会话占 12 个连接。并发申请以数据库事务或等效原子操作限制，不能两个请求同时越过额度；租约超时、页面关闭和 MediaMTX 会话对账均可回收连接。
3. 已建立的 WebRTC 媒体连接不会因之后一次 HTTP 鉴权失败而自动结束。经受限 `/internal/v1/inbox` 幂等接收 auth-service 的 `session.revoked` 后，media-service 结束关联的播放会话；租约到期也停止播放。HLS 每次新请求仍需鉴权。
4. 前端 HLS 播放器使用同源受保护地址，并让分片请求携带会话 Cookie；不要用无法附带受控鉴权信息的公开 iframe。WHEP 信令由同源 HTTPS 发起，媒体 UDP 连接单独处理。
5. 额度已满返回可识别错误，页面明确显示“观看容量已满”，不把它误报为摄像头故障。

### 4.3 交付物与阶段验收

- 播放授权、租约和 Nginx/MediaMTX 受控入口合同。
- 两个账号互相不能读取媒体；知道 `stream_id` 也不能绕过授权。直接访问 MediaMTX HTTP/控制 API 被拒绝。
- 12 路来源与 24 个观看连接的边界可观察；第 13 条活动源或第 25 个观看连接得到明确拒绝，租约过期后额度恢复。
- 登出、改密、租约到期、浏览器异常关闭后，已占观看连接最终释放。

---

## 五、第四阶段：Atlas 200I DK A2 流与浏览器播放

### 5.1 边缘输出约定

| 参数 | 首发值 |
| --- | --- |
| 每台 Atlas 200I DK A2 | 默认 1 路处理后 RTSP 预览流；算法可使用与展示不同的输入分辨率。 |
| 编码 | H.264/AVC Baseline、8-bit 4:2:0、无 B 帧；首发不含音频。 |
| 展示流 | 1280 × 720、15 fps、目标码率 1.5 Mb/s、关键帧间隔约 2 秒。 |
| 媒体拓扑 | 后端与 Atlas 200I DK A2 同园区可路由内网；MediaMTX 拉 RTSP。园区桌面用 WebRTC UDP 8189；UDP 不通或园区外桌面用 HTTPS HLS。 |

华为官方开发指南为 Atlas 200I DK A2 提供 VENC 视频编码接口，可编码 H.264/H.265；这不能直接证明算法推理、叠加和编码组合的实际帧率。H.264 B 帧在浏览器 WebRTC 中不兼容，需在设备侧配置兼容参数并实测。MediaMTX 首发只做拉流与分发，不承担统一后端转码。[Atlas 200I DK A2 VENC 应用开发指南](https://www.hiascend.com/document/detail/zh/Atlas200IDKA2DeveloperKit/23.0.RC2/Application%20Development%20Guide/aadgc/aclcppdevg_000033.html)、[MediaMTX WebRTC 说明](https://mediamtx.org/docs/features/webrtc-specific-features)

### 5.2 实现任务

1. MediaMTX 原生进程直接监听宿主机 UDP 8189；仅向配置的园区网段开放该端口。MediaMTX API、RTSP、HLS 与 WHEP 端口通过宿主机网关供本机 Compose 容器调用，宿主机防火墙拒绝其他来源。首发不部署 STUN/TURN，也不向公网开放 WebRTC UDP。Nginx 以 HTTPS 提供信令和 HLS。[MediaMTX WebRTC 网络配置](https://mediamtx.org/docs/features/webrtc-specific-features)
2. `/monitor` 页面每格先尝试 WebRTC，连接失败再用 HLS；UI 区分连接中、设备离线、RTSP 拉流失败、编码不兼容、UDP 不通、HLS 回退和额度已满。
3. 两个完整 12 分屏在园区内网观看时，12 路约为 18 Mb/s 入站净载荷、36 Mb/s 服务端下行净载荷；后端使用千兆内网接口。若两个大屏经公网 HLS，园区公网可用上行至少 100 Mb/s。实际联调检查 CPU、内存、带宽和浏览器解码负载，不把参数表当作已测性能结果。
4. 手机 PWA 不呈现监控大屏；历史告警依靠 event-service 的事件和 24 小时截图，不在本服务提供录像回放。

### 5.3 交付物与阶段验收

- Atlas 200I DK A2 → RTSP → MediaMTX → WebRTC/HLS 的可复现联调说明和浏览器状态展示。
- 目标 Atlas 200I DK A2 与园区网络上完成 12 路拉取和两个完整大屏同时观看；禁用 UDP 时，受控 HLS 可回退；不兼容编码有明确错误。
- PWA 手机入口不出现 `/monitor` 大屏；园区外桌面不会因没有公网 UDP 而错误声称 WebRTC 已连接。

---

## 六、第五阶段：运行、故障与交付

### 6.1 运行任务

1. 指标覆盖活动源流、观看连接、配额拒绝、RTSP 来源失败、WebRTC/HLS 成功与失败、租约回收、MediaMTX 对账差异。日志使用 request_id、camera_id 和 stream_id 串联，不记录来源凭据。
2. PostgreSQL 或 MediaMTX 不可用时，明确暴露就绪与播放错误；media-service 恢复后重建期望路径，不需要用户重复录入配置。
3. 流来源配置变更后旧来源停止、观看者得到可解释的中断或重连；配置失败保留版本与错误，避免显示假在线。
4. 备份 `media` schema 与 MediaMTX 必要配置；密钥轮换与恢复操作须保留来源凭据可用性，但不导出明文给前端。

### 6.2 最终交付物与验收

- media-service 源码、数据库迁移、可运行 Docker 镜像、Nginx/MediaMTX 受控配置、接口合同与部署说明。
- 可复现记录：账号越权、MediaMTX 重启、RTSP 中断、浏览器 UDP 阻断、HLS 回退、配额竞争、登出会话终止和租约回收。
- 视频能力只覆盖实时监控；录像回放、云转码、移动端大屏和跨园区媒体网络明确不在首发交付范围。

## 七、变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-10-19 | 明确 Nginx 无请求体授权子请求通过 `X-Original-URI` 调用内部 GET 播放校验，并要求先由 auth-service 验证会话及写请求 CSRF。 |
| 2026-10-04 | 从总技术规格拆分 media-service 实现任务，明确流配置、MediaMTX 对账、观看授权与配额、Atlas 200I DK A2 视频参数及阶段验收。 |
