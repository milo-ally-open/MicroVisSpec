# 行为规格：原生轻量部署

> 上游：[全局技术规格](../../Analysis/SPEC.md)、[系统结构](../../Design/STRUCTURE.md)。历史容器验收记录不代表本规格已通过。

## 范围和术语

首发进程为五个 Go 服务、Nginx/Web、PostgreSQL、Mosquitto 与 MediaMTX。MCP 未实现，不部署。首发原生部署目标为 Ubuntu/Debian Linux amd64；Compose 和 Dockerfile 不属于交付运行方式。

## 需求与验收条件

### REQ-01：独立原生服务与静态 Web 部署

五个已实现 Go 服务应各自构建为一个可执行二进制，systemd 独立管理。前端经 Node/Vite 构建为静态目录，由系统 Nginx 托管。

- **AC-01.1** Given Go 1.27+ 和源码，When 执行 `project/src/services/build.sh`，Then `auth-service`、`device-service`、`event-service`、`media-service`、`push-service` 五个 Linux amd64 二进制生成于被 Git 忽略的 `services/bin/linux-amd64/`。
- **AC-01.2** Given 正确的服务环境文件和基础依赖，When 启用五个 systemd unit，Then 每个服务独立启动、可查看状态/日志，并通过 `/health/live` 和 `/health/ready` 报告自身状态。
- **AC-01.3** Given Node.js 24+，When 在 `project/src/clients/web` 执行 `npm ci && npm run build`，Then Vue/PWA 静态文件生成至 `dist/`，可复制到 Nginx web root。
- **AC-01.4** Given Nginx 配置、TLS 文件和前端 dist，When 执行 `nginx -t` 并重载，Then HTTPS 提供静态站点并按同源路径代理 API、edge 与媒体授权请求；无 Docker DNS 或 Docker host gateway 依赖。
- **AC-01.5** Given一台未安装 Docker 的支持主机，When 按部署手册安装系统依赖并部署，Then 前后端服务可独立升级、重启；部署指令不调用 Docker 或 Compose。

### REQ-02：数据库角色、迁移和持久化

PostgreSQL 使用五个独立角色及各自 schema，迁移权限与服务数据所有权保持不变。

- **AC-02.1** Given全新数据库，When 执行 `auth-service/database-bootstrap.sh`，Then `auth`、`device`、`event`、`media`、`push` schema 与同名服务角色正确创建。
- **AC-02.2** Given五个数据库 URL，When 服务连接数据库，Then 各服务可迁移和访问自己的 schema，不能访问其他服务 schema。
- **AC-02.3** Given系统服务重启或升级，When 服务恢复，Then PostgreSQL 与 Mosquitto 数据不被部署步骤删除；备份/恢复文档明确保留必要机密并排除临时截图长期归档。

### REQ-03：边缘 MQTT/TLS 与 ACL

Mosquitto 原生服务保留设备身份隔离、TLS 与 Dynamic Security 行为。

- **AC-03.1** Given broker TLS 证书、管理员密码与 Dynamic Security 插件，When 启动 Mosquitto，Then TLS listener 可用、匿名连接关闭，状态写入系统持久目录。
- **AC-03.2** Given未认证客户端或设备 A，When连接/访问设备 B 的 topic，Then请求被拒绝；授权设备只能按消息规格访问自己的 topic。
- **AC-03.3** Given设备服务管理员身份，When启动 MQTT bootstrap 初始化或重试，Then两个平台服务角色和最小 ACL 建立且重复执行幂等。
- **AC-03.4** Given Atlas 设备通过部署的 DNS 和 CA 访问 broker，When进行 MQTT/TLS 通信，Then服务端证书验证和合法 topic 流量通过；尚未实测项明确标记待 Atlas 验收。

### REQ-04：浏览器入口和身份边界

Nginx 是唯一 Web/API/媒体 HTTP 入口。Go 服务监听仅本机可达的独立端口。

- **AC-04.1** Given浏览器访问 Web/API，When请求静态页面或 `/api/v1`，Then由 Nginx 同源服务并代理到正确的 loopback Go listener。
- **AC-04.2** Given未登录、伪造身份头或错误 CSRF/Origin，When请求受保护 API，Then现有认证拒绝规则保持有效，Nginx 覆盖客户端传入的身份头。
- **AC-04.3** Given已撤销会话，When重试合法同源 logout，Then遵守 auth-service 合同清除 Cookie 并成功返回。
- **AC-04.4** Given设备凭据，When访问 `/edge/v1` 激活或证据上传，Then继续由 device-service/event-service 验证设备身份，不要求浏览器登录。
- **AC-04.5** Given外部网络端口检查，When检查 DB、Go、Mosquitto 管理和 MediaMTX 控制端口，Then这些端口不对非本机暴露；仅开放运维确认的 HTTPS、园区 MQTT/TLS 和 WebRTC ICE 端口。

### REQ-05：逐请求授权媒体代理

WebRTC/WHEP 与 HLS 仍逐请求进行会话和有效播放租约授权，再由 Nginx 代理到主机 MediaMTX。

- **AC-05.1** Given有效播放租约，When请求 WHEP/HLS，Then每个信令、清单及子资源请求都完成授权并能正常播放。
- **AC-05.2** Given没有会话、无效账号或过期租约，When请求媒体资源，Then不转发至 MediaMTX 且不泄露来源 RTSP URL/凭据。
- **AC-05.3** Given非本机请求访问 MediaMTX API/RTSP 管理接口，When连接主机端口，Then主机网络规则拒绝访问。
- **AC-05.4** Given园区网络与终端兼容，When建立浏览器实时流，Then MediaMTX 原生进程使用部署配置端口服务；实际 Atlas 编码兼容与吞吐单独记录验收。

### REQ-06：机密与配置

机密独立于代码与普通环境值保存，服务缺少必要配置时失败关闭。

- **AC-06.1** Given机密文件缺失、空白或不可读，When启动服务，Then服务未 ready，日志不输出机密值。
- **AC-06.2** Given应用环境文件，When部署，Then它只含非机密配置和 `*_FILE` 路径；敏感文件位于受限 `/etc/microvisspec`，Git 不跟踪任何本地 `.env` 或 `.secret-*`。
- **AC-06.3** Given证书/服务令牌/数据库密码轮换，When更新授权文件并重启依赖 unit，Then旧凭据可撤销，恢复顺序清晰，不触及数据库或证据数据。

### REQ-07：原生运维、备份和状态记录

仓库文档描述构建、安装、状态、日志、停止、备份/恢复及 smoke 步骤。未运行的验证不得标记通过。

- **AC-07.1** Given干净 Linux 主机，When按 DEPLOY.md 操作，Then部署无需 Docker、Compose 或工作区外隐式配置。
- **AC-07.2** Given完整服务运行，When执行 smoke，Then可检查 HTTPS、健康状态、认证拒绝、登录/退出、设备 edge 路由和媒体未授权拒绝。
- **AC-07.3** Given有效 PostgreSQL 备份和隔离恢复环境，When恢复数据库与必要配置，Then五服务 readiness 通过；恢复过程不得清除原有数据。
- **AC-07.4** Given Atlas 或真实园区网络未实测项，When发布验收记录，Then明确标待实机验证，不以主机构建结果替代。
- **AC-07.5** Given Docker Desktop 中仅属于本项目且均已退出的容器，When本地清理，Then仅删除项目容器和确认无引用的项目镜像，记录前后 `docker system df` 差值，所有数据卷与其它项目资源保持不变。

## 不变项与追踪

浏览器/API/edge/MQTT/event/media 业务合同由全局及对应服务规格定义。本部署规格只约束运行方式，不重新定义 payload。

| REQ | 主要实现 | 任务 | 状态 |
|---|---|---|---|
| REQ-01 | 五个 systemd unit、build.sh、Nginx、Web dist | T-01、T-04 | 实现完成；目标机启动待验收 |
| REQ-02 | PostgreSQL、数据库 bootstrap | T-02 | 脚本与文档完成；目标机恢复待验收 |
| REQ-03 | device-service Mosquitto 配置/bootstrap | T-03 | 配置与脚本完成；Atlas/TLS 待验收 |
| REQ-04 | Nginx 与五个 loopback listener | T-04 | 配置完成；目标机 Nginx 检查待执行 |
| REQ-05 | Nginx、media-service、MediaMTX | T-05 | 配置完成；媒体链路待验收 |
| REQ-06 | `*_FILE`、部署机配置和权限 | T-06 | 样例与权限说明完成 |
| REQ-07 | DEPLOY.md、验收记录、Docker 清理 | T-07、T-08 | 文档/清理完成；目标机 smoke 待验收 |

## 变更记录

| 日期 | 内容 |
|---|---|
| 2026-10-26 | 以原生 systemd/Linux 部署合同取代 Compose 合同。 |
