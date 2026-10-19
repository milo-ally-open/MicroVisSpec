# 提案：首发平台部署与整合运行

> 状态：实现中<br>
> 路径：跨边界高风险<br>
> 负责人：平台工程<br>
> 相关产品需求 / 上游合同：[PRD](../../Analysis/PRD.md)、[全局技术 SPEC §2.1、§7.3](../../Analysis/SPEC.md)、[系统结构 §2、§8](../../Design/STRUCTURE.md)

## 背景与问题

首发需要 auth、device、event、media、push 五个 Go 服务，以及 Vue/PWA、PostgreSQL、Mosquitto 和 MediaMTX 协同运行。目前仓库有各服务源码、Dockerfile 与服务内测试，但没有 Compose 编排、Nginx 入口、初始化与操作手册；`docker compose ps` 因找不到 Compose 文件不能列出应用栈。当前服务只具备单体内的 handler/数据库集成测试，尚无可复现的容器整合验收。

这使登录态/CSRF、服务间令牌、MQTT Dynamic Security、私有媒体路由、数据库 schema 权限及证据卷权限无法在真实网络边界下验证。

## 目标与成功观察

- 运维人员按仓库说明生成本地部署机密并启动完整首发栈；健康检查、服务依赖和数据卷状态可观察。
- 浏览器仅从 HTTPS 同源入口访问 Web/PWA 与 API；Atlas 通过设备 HTTPS 入口和 MQTT/TLS 接入。
- PostgreSQL、服务内部端口、Mosquitto 动态安全控制面、MediaMTX 控制 API 和证据卷不对宿主机网络发布。
- 各服务以独立 PostgreSQL 角色管理自己的 schema；部署与恢复步骤可以重复执行并有可检查证据。

## 范围

### 本次包含

- 首发部署拆分为两个独立 Compose 单元：`project/src/services/docker-compose.yaml` 编排五个 Go 服务、PostgreSQL、Mosquitto 与初始化任务；`project/src/clients/web/docker-compose.yaml` 单独构建并运行 Web/PWA 的 Nginx 容器。两者通过显式命名的共享 Docker 网络连接，Web 生命周期可独立管理。
- MediaMTX 继续作为 `media-service` 目录下的原生进程运行，不加入任何 Compose 文件。
- TLS/HTTPS、边缘 MQTT/TLS、身份授权子请求、边缘 HTTP 路由、WebRTC/HLS 私有代理及健康探针。
- 按最小权限提供本地 Compose secrets、PostgreSQL 初始化、MQTT 系统角色初始化、持久化和备份/恢复说明。
- 可复现的容器启动、网络暴露面、配置校验和整合冒烟验收。

### 本次不包含 / 保持不变

- 不新增业务微服务；mcp-service 继续延期，不构建、不部署。
- 不改变用户注册、设备协议、事件语义、告警规则默认关闭、截图 24 小时保留和闭环事件 90 天保留约定。
- 不把设备 MQTT、WebRTC 或 HLS 接口改为公网无限制访问；不引入对象存储、Kubernetes、STUN/TURN 或录像回放。
- 不宣称开发板实机视频/推理性能验收已完成；部署栈验收与 Atlas 场景性能验收分开记录。

## 现状依据与约束

- 代码依据：五服务 Dockerfile 分别构建 Go 可执行文件；服务启动通过环境变量读取数据库、服务令牌、MQTT、VAPID、加密密钥和媒体控制参数；所有服务已有 `/health/live` 和 `/health/ready` 路由。
- 代码依据：device-service 与 event-service 使用 MQTT/TLS 和 Dynamic Security；media-service 需要 PostgreSQL、device-service 与 MediaMTX 控制 API；event-service 需要 PostgreSQL、证据目录、MQTT、device-service 与 push-service。
- 部署现状：仓库没有 Compose/Nginx/Mosquitto/MediaMTX 配置；服务运行时当前依赖进程环境变量，须支持受控文件挂载而不将密钥写入镜像或提交文件。
- 已确认约束：工作在用户指定的 `dev` 分支，不新建分支或工作树；每个实现任务开始前先读关联服务 Spec。
- 已确认硬件：Atlas 200I DK A2 可联网；边缘 MQTT/TLS 需从板端网络实际连通检查。
- `[假设]` 首发整合目标为单台 Linux Docker 主机，使用 Docker Compose；当前 Windows Docker Desktop 用于开发/集成验证。若实际生产主机平台不同，路径权限、端口绑定和证书部署须在发布前复核。
- `[已确认]` 后端与 Web/PWA 分开部署；Web Compose 文件和 Dockerfile 均归 `src/clients/web`，不创建 `web-service` 目录。后端 Compose 先创建稳定命名的后端网络，Web Compose 再以 external network 加入；停止或重建 Web 不触发后端容器重建。
- `[假设]` 公开 TLS 证书由部署者提供；本地验证使用部署者准备的开发证书，浏览器需信任该证书后才能验证 Secure Cookie/PWA。证书及其他机密按 README 放入对应 service 目录的 Git 忽略文件，不由仓库脚本生成。

## 备选方案与建议

| 方案 | 收益 | 成本/风险 |
|---|---|---|
| Docker Compose 单机首发 | 与当前单机园区部署和 Docker Desktop 验证一致，服务、卷、网络及机密声明可一起审阅。 | 不提供多节点 HA；运维仍需负责主机补丁、证书和备份恢复演练。 |
| Kubernetes | 编排和扩缩容能力更强。 | 超出单园区首发部署复杂度，当前没有集群或运维基线。 |
| 仅提供手工 `docker run` 命令 | 初始文件少。 | 难以复现网络、健康依赖、机密和卷配置，无法满足可审阅的整链路验收。 |

建议：使用 Compose；镜像采用经构建和 API/配置验证的固定版本，不使用 `latest`；凭据由 Compose secrets 文件挂载，并由服务通过 `*_FILE` 配置读取。Compose secrets 在本地模式是只读文件挂载，部署目录仍需操作系统权限保护；生产机密管理由运维按同一文件合同提供。[Docker Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/)

## 跨边界评审结论

- [PRD](../../Analysis/PRD.md) 中的首发业务闭环与范围保持不变；Nginx、五个现有服务、数据库、broker 和媒体面沿用已批准的服务责任边界。
- 登录授权、edge 设备认证、媒体逐请求授权、账号 schema 隔离、机密生命周期和恢复均有专门 REQ/AC 与任务覆盖；mcp-service 和录像存档未纳入部署。
- 唯一已知接口差异是 media-service 授权入口与 Nginx 无 body 子请求不兼容。它由 T-05 依赖项处理：先回读服务 Spec、修订接口合同与验收，再实现；不得通过取消鉴权绕过。
- 生产域名、正式证书与备份目标不改变本地实现合同，已作为发布前运维输入记录，不阻塞可实现性。
- 结论：文档结构、范围、风险控制、需求/验收/任务映射符合高风险 Spec 门槛，进入实现。

## 影响面、依赖与风险

- 产品范围 / 服务 / API / 数据 / 部署 / 安全影响：五服务 HTTP API、共享 Postgres 实例内的 schema/角色、Mosquitto TLS/Dynamic Security、Nginx 登录授权与媒体代理、截图私有持久化卷。
- 依赖与兼容：当前服务配置缺少 `_FILE` 读取能力；服务内 `/health/ready` 将用于容器就绪条件；MediaMTX 当前受保护入口需与 `media-service` 的播放授权请求格式对齐。
- 风险：证书 SAN/客户端受信任链、Nginx auth_request 编译模块、容器 UID 与证据卷权限、Mosquitto ACL 过宽、仅保护媒体播放 API 而漏保护分片/信令、数据库初始化只适用于空 PostgreSQL 卷。
- 回滚或恢复：停止/回退 Compose 镜像与配置但保留数据卷；数据库变更遵守向后兼容，恢复前备份当前数据库与 broker/MediaMTX 配置；证据卷按 24 小时策略清理，不纳入长期备份。

## 未决问题

| 问题 | 影响 | 负责人/验证方式 | 状态 |
|---|---|---|---|
| 生产域名、证书签发方式与最终网卡绑定地址 | Nginx TLS 与 MQTT/WebRTC 端口对外可达范围 | 运维在生产部署前提供；本次开发用显式配置项和开发证书 | 非阻塞，待部署确认 |
| 部署主机备份存储位置和恢复目标时间 | 备份保留与演练计划 | 运维发布前确认；本 Spec 先提供可验证的备份/恢复命令 | 非阻塞，待部署确认 |

## 变更记录

| 日期 | 内容 | 决定/依据 |
|---|---|---|
| 2026-10-19 | 建立首发 Compose 部署与整合运行范围 | 全局技术 SPEC §2.1、§7.3 与当前仓库部署文件缺失现状 |
| 2026-10-19 | 将 Web/Nginx 部署从后端 Compose 拆出，单独放在 `src/clients/web/docker-compose.yaml`；共享 external backend network，保持部署生命周期分离 | 用户明确要求前后端分离部署，且 Web Compose 归 clients |
