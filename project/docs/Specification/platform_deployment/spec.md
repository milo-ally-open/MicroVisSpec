# 行为规格：首发平台部署与整合运行

> 关联提案：[proposal.md](proposal.md)<br>
> 本文档组状态见 `proposal.md`。<br>
> 来源：用户授权按既有 Spec 实现首发平台；[全局技术 SPEC](../../Analysis/SPEC.md)、[系统结构](../../Design/STRUCTURE.md) 与各服务 Spec。

## 范围和术语

本规格定义源码树中的两个独立 Compose 部署单元与 MediaMTX 原生进程如何启动、相互通信、对外开放、保管持久数据并执行整合验收。后端 Compose 位于 `project/src/services/docker-compose.yaml`，只编排后端服务、数据库、broker 和初始化任务；Web Compose 位于 `project/src/clients/web/docker-compose.yaml`，只构建并运行 Nginx/Web/PWA。两者通过稳定命名的 external Docker network 通信，后端须先创建网络并就绪，Web 生命周期可独立管理。浏览器入口由 Nginx 提供；边缘端使用 HTTPS 与 MQTT/TLS；PostgreSQL、业务服务端口、Mosquitto 管理控制和 MediaMTX 控制面是内部依赖。MediaMTX 使用 `project/src/services/media-service/mediamtx` 与同目录 `mediamtx.yml`，不构建自定义镜像或作为 Compose 服务运行。`ready` 表示服务依赖已可处理业务，`live` 仅表示进程存活。

## 需求与验收条件

### REQ-01：源码内可部署的完整首发服务拓扑与可重复启动

**需求**：`project/src/services/docker-compose.yaml` 只构建并启动 auth、device、event、media、push、PostgreSQL、Mosquitto 与初始化任务，不包含 Web/Nginx。`project/src/clients/web/docker-compose.yaml` 单独构建并启动名为 `web` 的 Nginx 容器，使用同目录 `Dockerfile` 和 Nginx 配置，发布配置的 HTTP/HTTPS 端口，并加入由后端 Compose 创建的 external backend network，以服务名访问后端 API 和 MediaMTX 代理。两份 Compose 可分别校验、构建、启动、停止和重建；首次启动顺序为后端就绪后再启动 Web，Web 健康只表示 Nginx 与静态入口可用，不替代后端 readiness。MediaMTX 使用 `project/src/services/media-service/mediamtx` 原生可执行文件和同目录 `mediamtx.yml`，由运维直接启动；容器通过宿主机网关访问其内部控制/HTTP 端口，宿主机防火墙只允许本机 Compose 网络访问内部端口。`src/services` 根目录只放后端 Compose 和平台级说明/环境模板等文件；其所有子目录均为 `*-service`，PostgreSQL 初始化文件属于 `auth-service`，Mosquitto 配置属于 `device-service`，MediaMTX 属于 `media-service`。Web 的 Dockerfile、独立 Compose、Nginx 配置、Web 专属环境模板和运行机密属于 Web 客户端源码；不设置独立 deploy、postgres、mosquitto、mediamtx、scripts、secrets 或 `web-service` 目录。运维步骤以简洁文档和直接命令为准，不提供 PowerShell 运维脚本。mcp-service 不属于本次拓扑。服务启动必须等待必要依赖就绪，使用容器健康探针区分存活与就绪。

**验收条件**

- **AC-01.1** Given 所需后端配置、服务机密和 MediaMTX 原生可执行文件已准备，When 校验 `mediamtx.yml` 并启动 MediaMTX 后执行后端 Compose 构建与启动，Then 后端 Compose 能解析并启动所有后端/基础设施容器，五个 Go 服务镜像均成功构建，依赖按健康状态启动且所有 required container 最终为 healthy；media-service ready 确认宿主机 MediaMTX API 可达。
- **AC-01.2** Given 任一必需依赖不可用或配置缺失，When 启动对应服务，Then 它不能显示 ready；容器健康状态与服务 `/health/ready` 一致，依赖恢复后可自动就绪。
- **AC-01.3** Given 首次空数据卷，When 启动完整栈，Then PostgreSQL 完成受限角色/schema 初始化，各 Go 服务只在自己的 schema 执行迁移，Mosquitto broker 与 MediaMTX 控制面在就绪检查通过后可被依赖服务使用。
- **AC-01.4** Given 后端源码、后端 `.env` 和各 service secret files，When 从 `project/src/services/` 校验并构建后端 Compose，Then 它不包含 Web/Nginx，五个 Go 服务镜像能从源码构建，PostgreSQL 初始化文件在 auth-service、Mosquitto 配置在 device-service，MediaMTX 使用 media-service 下原生二进制和 YAML；`src/services` 除根级文件外仅需要 `*-service` 子目录。
- **AC-01.5** Given 后端 Compose 已创建共享网络并且后端服务已就绪，When 从 `project/src/clients/web/` 单独校验、构建、启动或停止 Web Compose，Then Web/Nginx 可独立发布 HTTP/HTTPS 并经 external backend network 访问后端；Web Compose 操作不构建、停止或重建后端容器，且不需要 `web-service` 目录。

**验证方式**：分别对两份 Compose 执行 `docker compose config --quiet` 和镜像 build；检查后端健康状态、共享网络、Web 独立生命周期、容器日志、数据库角色/schema 查询。

### REQ-02：数据库隔离、持久化和迁移所有权

**需求**：PostgreSQL 使用持久化卷、仅内部监听；auth、device、event、media、push 各使用独立数据库角色，仅拥有本服务 schema 的权限。数据库初始化只负责角色/schema 和默认权限，服务仍由自己的启动逻辑迁移自有表。

**实现约束**：当前 GORM PostgreSQL migrator 在每次服务迁移时执行 `CREATE SCHEMA IF NOT EXISTS`，即使目标服务 schema 已存在。因此各服务角色需要 PostgreSQL 数据库级 `CREATE` 权限才能自行迁移。该权限允许角色创建额外空 schema；它不授予其他服务 schema 的 `USAGE`、表权限或所有权。验收仍须验证服务角色不能读取或修改其他服务 schema。若后续移除 GORM 的 schema 创建语句，应同步撤销数据库级 `CREATE`。

**验收条件**

- **AC-02.1** Given 全新数据库卷，When 执行数据库初始化并启动服务，Then 五个 schema 存在且分别归属对应服务角色；角色无权读取或修改其他服务 schema。
- **AC-02.2** Given 服务数据库角色，When 查询服务自有 schema，Then 可完成迁移和 CRUD；When 查询其他服务 schema，Then PostgreSQL 拒绝访问。
- **AC-02.3** Given 容器重启或重建，When 服务恢复，Then 数据仍在持久卷中；备份/恢复步骤能还原 schema、角色/必要配置并通过就绪检查。截图卷不进入长期备份，过期与授权行为仍由 event-service 执行。

**验证方式**：隔离角色 SQL 检查、服务数据库集成测试、容器重建后持久性检查、隔离环境备份与恢复演练。

### REQ-03：边缘 MQTT/TLS 身份与消息 ACL

**需求**：Mosquitto 使用 TLS 监听和持久存储；未认证连接及匿名访问一律拒绝；device-service 管理凭据只能操作 Dynamic Security，系统服务与每台设备使用独立 client_id、凭据和最小主题 ACL。broker TLS 证书须包含 `mosquitto` 内部名称并由服务/Atlas 信任。

**验收条件**

- **AC-03.1** Given 空数据卷和有效机密，When broker 完成初始化，Then TLS listener 健康、Dynamic Security 已初始化、持久配置/会话写入卷中，MQTT 明文 listener 不存在。
- **AC-03.2** Given 未认证客户端或设备 A 的身份，When 连接或访问设备 B 的主题，Then 未认证连接被拒绝，设备 A 无法发布/订阅设备 B 的主题；授权设备可执行其定义范围内的 QoS 1 心跳、命令 ACK、事件上报与事件 ACK。
- **AC-03.3** Given device-service 的管理身份，When 创建或停用设备身份，Then Dynamic Security 生效且重启后 ACL 保留；动态安全控制凭据与管理 listener 不公开到宿主机外部。
- **AC-03.4** Given Atlas 位于已配置园区网络，When 使用 broker CA 与设备凭据访问宿主机 TLS MQTT 端口，Then TLS 主机名验证通过且合法主题流量可达；不使用跳过证书验证的配置。

**验证方式**：Mosquitto 配置校验、匿名/跨设备 ACL 负向测试、device-service Dynamic Security 集成测试、Atlas 200I DK A2 TLS 实机连通记录。

### REQ-04：唯一浏览器入口与身份边界

**需求**：独立 Web Compose 中的 Nginx 对同源 HTTPS 提供 Web/PWA、浏览器 API、设备 HTTP 入口和媒体代理，并通过 external backend network 访问后端服务。它必须清除客户端身份头、执行 auth-service 会话/CSRF 授权子请求，并按全局技术 SPEC 对匿名登录/注册、重复登出、public VAPID key、设备 `/edge/v1/activate` 和截图上传分别处理。业务服务与敏感控制端口不得发布到宿主机。

**验收条件**

- **AC-04.1** Given 浏览器请求，When 访问首页和静态资源，Then Nginx 提供 Vue 应用；API 维持同源 `/api/v1` 路径，正确代理至对应服务，不暴露上游服务端口。
- **AC-04.2** Given 未登录、伪造的 `X-Account-ID`/`X-Session-ID` 或不匹配的 CSRF/Origin，When 访问受保护 API，Then 请求按 401/403 拒绝且后端不能收到客户端伪造的身份；合法登录后请求收到 auth-service 返回的可信身份头。
- **AC-04.3** Given 已撤销会话，When 浏览器重复调用合法同源 logout，Then 请求仍到达 auth-service 并按其契约清除 Cookie、返回成功，不被通用 auth_request 抢先拒绝。
- **AC-04.4** Given edge device credentials，When 调用激活或上传证据，Then 请求到达 device-service/event-service 的专用 `/edge/v1` 入口且不要求浏览器会话或 CSRF；事件截图仍由 event-service 校验设备凭据。
- **AC-04.5** Given 宿主机端口扫描，When 访问 PostgreSQL、五个 Go 服务、Mosquitto 管理控制面、MediaMTX API/RTSP/HLS/WHEP 端口，Then 两份 Compose 均不发布这些端口且宿主机防火墙仅允许本机 Compose 网络访问；外部只可触达 Web Compose 配置的 Nginx HTTPS/HTTP 重定向、后端 Compose 配置的园区 MQTT/TLS 和园区 WebRTC UDP 端口。

**验证方式**：Nginx 配置测试、TLS/API 黑盒请求、请求头伪造测试、宿主机端口映射盘点。

### REQ-05：受授权保护的媒体转发

**需求**：WebRTC 信令和 HLS 清单、分片、密钥/子资源请求只能经 Nginx 转发并逐请求核对账号、会话、stream_id 和有效 viewer lease；Nginx 到 media-service 的授权使用内部令牌，媒体控制 API 仅由 media-service 访问。媒体请求不得依赖跨域公开 iframe 或浏览器携带 RTSP 凭据。

**验收条件**

- **AC-05.1** Given 有效播放租约，When 浏览器通过 HTTPS 发起 WHEP 或 HLS 请求，Then 每个媒体 HTTP 请求经授权子请求检查后转发，播放正常且分片请求携带同源会话 Cookie。
- **AC-05.2** Given 无会话、错误账号、已释放/过期租约或伪造 stream/lease ID，When 请求信令、清单或任一分片，Then Nginx 不把媒体请求转发给 MediaMTX，且不泄露来源 RTSP URL/凭据。
- **AC-05.3** Given MediaMTX 控制 API 被宿主机外部网络访问，When 发起请求，Then 宿主机防火墙阻止该连接；media-service 通过宿主机网关并使用专用凭据检查、恢复路径并回收已结束会话。
- **AC-05.4** Given 园区内 WebRTC 网络与媒体编码兼容，When 连接实时画面，Then 原生 MediaMTX 进程直接监听并使用宿主机配置的 UDP ICE 端口；WebRTC 不可用时同源 HTTPS HLS 可回退。首发浏览器兼容、Atlas VENC 和多路吞吐仍按媒体/硬件 Spec 单独验收。

**验证方式**：WHEP/HLS 浏览器联调、租约有效/无效授权测试、MediaMTX API 隔离及 UDP 端口映射检查。

### REQ-06：机密、配置和生命周期操作

- **AC-06.5** Given Windows Git 检出并由 Compose 只读挂载初始化脚本，When PostgreSQL/MQTT bootstrap 容器执行脚本，Then shell 文件保持 LF，不能因 CRLF 产生 `illegal option` 并退出；初始化可在已创建的本机测试卷上重试。

**需求**：服务令牌、数据库密码、MQTT 凭据、加密/签名密钥、VAPID 私钥及 TLS 私钥由 Compose secrets 文件只读提供给明确授权的容器。Go 服务可以通过 `*_FILE` 读取敏感配置；配置缺失或格式不合法时应启动失败并给出不含 secret 内容的错误。生产部署不依赖仓库中的真实 secret 文件。

**验收条件**

- **AC-06.1** Given secrets 未提供或不可读，When 执行 Compose 校验/启动，Then 部署明确失败，不以空值/开发默认密钥替代，不在日志或命令输出中打印密钥内容。
- **AC-06.2** Given任一容器，When 枚举其 `/run/secrets`，Then 只挂载该 Compose 单元内该容器所需 secret；敏感值不位于镜像层、版本库、普通环境变量或 API 响应，`.env`、各 `*-service/.secret-*` 及 `clients/web/.secret-*` 文件被 Git/Docker build context 排除，不建立集中 secrets 目录。
- **AC-06.3** Given 本地开发机，When 运维按服务文档配置各 owner 目录的 `.secret-*` 文件，Then 本地 TLS/header 文件位于 Web 客户端源码目录，MQTT 证书、跨服务匹配令牌、数据库 URL、VAPID 密钥对和加密/签名密钥位于对应 service 目录且未跟踪；部署者负责保管与轮换机密，文档不提供自动生成或覆盖机密的运维脚本。
- **AC-06.4** Given部署人员轮换机密，When 更新机密和重启依赖服务，Then旧密钥按明确步骤撤销，新旧兼容窗口和失败恢复步骤可操作；数据库备份和证据保留口径不混淆。

**验证方式**：Compose secrets 授权审查、Git 忽略/镜像扫描、配置缺失测试、各服务机密文件轮换演练。

### REQ-07：运维健康、备份及整合验收

**需求**：仓库应通过简洁的 service README 提供开发机和部署人员可执行的配置校验、启动、状态、日志、停止、备份/恢复和 smoke-test 命令；不提供 PowerShell 运维脚本，验收记录需区分容器集成、服务内集成测试、Atlas 实机与未执行项。

**验收条件**

- **AC-07.1** Given新的 Docker Desktop/Linux Compose 环境，When 按文档先启动后端 Compose、再启动 Web Compose，Then 可以识别两个部署单元各自的健康状态、检查关键日志和失败容器，命令不依赖工作树外的隐式配置。
- **AC-07.2** Given运行中的栈，When执行 smoke tests，Then验证首页 HTTPS、证书、未授权拦截、登录/退出、各服务就绪、设备激活路由、截图 edge 路由和媒体未授权拒绝；失败时有具体诊断输出。
- **AC-07.3** Given备份文件和空的隔离数据卷，When执行恢复演练，Then服务恢复自己的数据与 schema，敏感配置按备份外机密重新注入，所有 required readiness 检查通过。
- **AC-07.4** Given 尚未在 Atlas 或真实园区网络验证的项，When 更新交付记录，Then明确标为待实机验证，不以 Docker Desktop 的模拟结果替代硬件证据。

**验证方式**：按 README 执行的 Compose/manual smoke 命令、全量 Go service tests、Web client type-check/test/build、独立数据库恢复演练和板端联网验证。

## 不变项与兼容要求

- 用户可观察的浏览器 API 与 edge API 路径、cookie/CSRF、事件幂等、服务内数据所有权、MQTT Topic 与媒体播放合同继续由全局与服务 Spec 定义。
- Postgres 仍是单实例、五 schema、五角色；运行服务不使用数据库超级用户；mcp-service 不部署。
- 外部端口可按部署者填写的接口地址显式绑定，但默认值不得意外开放数据库、服务内部端口、MQTT 动态安全控制主题、媒体控制 API 或截图目录。
- 图片目录仅保存 event-service 需要的临时证据；PWA 不缓存 API、媒体或截图。

## 数据处理和保留

- PostgreSQL、Mosquitto 动态安全状态及会话数据使用命名卷持久化；MediaMTX 控制配置可由 media-service 重建，MediaMTX 重启不成为路径配置的唯一数据来源。
- event-service 证据卷独立挂载，使用非 root 服务 UID/GID 可写；截图上传后 24 小时过期，按 service Spec 授权与清理，不进入长期数据库备份。
- 数据库备份在应用静止或一致性备份方式下生成；备份加密与异地保存由运维配置，恢复必须在隔离卷演练。

## 明确不做

- Kubernetes/多机 HA/数据库复制、STUN/TURN、录像存档、公网直连 RTSP、对象存储、mcp-service 部署。
- 由 Compose 自动替用户取得正式公有证书、DNS 或防火墙配置。
- 将模拟器或 Docker 容器性能作为 Atlas 硬件验收证据。

## 需求追踪

| 上游条款 | REQ | 模块/服务 | 任务 ID | AC | 验收证据/状态 |
|---|---|---|---|---|---|
| 全局 SPEC §2.1、§7.3；STRUCTURE §2、§8 | REQ-01 | 后端 Compose、Web Compose、五服务 Dockerfile、Nginx、MediaMTX 原生进程 | T-01、T-04、T-06 | AC-01.1–01.5 | 待执行 |
| 全局 SPEC §2.3、§7.3 | REQ-02 | PostgreSQL 初始化、五 Go 服务、数据卷/备份 | T-02 | AC-02.1–02.3 | 待执行 |
| 全局 SPEC §2.1、§7.2；device/event Spec | REQ-03 | device-service 下 Mosquitto、device/event、Atlas | T-03 | AC-03.1–03.4 | 待执行 |
| 全局 SPEC §2.1、§2.2、§7.2；auth/Web Spec | REQ-04 | Web Compose/Nginx、auth/device/event/push | T-04 | AC-04.1–04.5 | 待执行 |
| 全局 SPEC §3.5、§7.2；media Spec | REQ-05 | Nginx、media-service、MediaMTX、Web | T-05 | AC-05.1–05.4 | 待执行 |
| 全局 SPEC §2.2、§7.2–7.3 | REQ-06 | Compose secrets、配置加载、service README | T-06 | AC-06.1–06.4 | 待执行 |
| 全局 SPEC §7.3；全部服务、边缘和 Web Specs | REQ-07 | service README 运维命令、双 Compose/测试 | T-07 | AC-07.1–07.4 | 待执行 |

## 假设与未决问题

- `[假设]` Docker Compose 支持当前文件秘密、服务健康依赖与 Docker Desktop Linux 容器；实际 Compose 版本由部署启动检查验证。
- `[待确认]` 生产外部域名、正式证书和具体宿主机网卡绑定地址；本次不阻塞开发部署栈。
- `[待确认]` 备份目标目录、加密和留存期限由生产运维确认；开发环境使用本地备份目录进行恢复演练。

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-19 | 初稿；建立首发 Compose、网络、安全和整合验收需求。 |
| 2026-10-19 | 明确源码内可部署约束：Compose/运行文件位于 `src/services`，Web 镜像 Dockerfile 位于 `src/clients/web`，不设置独立部署目录。 |
| 2026-10-19 | MediaMTX 改为 `media-service/mediamtx` + `media-service/mediamtx.yml` 宿主机原生进程，不再构建或运行 MediaMTX 容器；Compose 网络由宿主机网关连接，并由宿主机防火墙约束内部端口。 |
| 2026-10-19 | Web/Nginx 从后端 Compose 拆分为 `src/clients/web/docker-compose.yaml`，使用 external backend network 独立管理。 |
