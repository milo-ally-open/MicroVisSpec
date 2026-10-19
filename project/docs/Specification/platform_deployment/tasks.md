# 实施任务：首发平台部署与整合运行

> 关联规格：[spec.md](spec.md)；关联设计：[design.md](design.md)<br>
> 执行状态以本文件 checkbox 与验收记录为准。用户要求直接在 `dev` 工作，不创建分支/工作树。

## 任务清单

### 1. 合同与运行基础

- [ ] **T-01 后端 Compose 骨架、镜像与服务机密接线** — 目标模块/文件：`project/src/services/docker-compose.yaml`、`.env.example`、owner service 目录、各服务 Go config/Dockerfile；交付：后端服务与基础设施拓扑（不含 Web/Nginx）、稳定命名的 backend network、最小权限文件 secret、`*_FILE` secret loader、容器健康检查与全子目录 `*-service` 的源码部署布局；运维前置条件和命令写在简洁 README，不提供 PowerShell 运维脚本或集中脚本目录；MediaMTX 采用 `media-service/mediamtx` + `mediamtx.yml` 原生进程，不构建容器、不增加下载/启动包装脚本；覆盖：REQ-01 / AC-01.1、AC-01.2、AC-01.3、AC-01.4；REQ-06 / AC-06.1, AC-06.2；依赖：无；每次改 Go 服务前回读其服务 Spec。
- [ ] **T-02 PostgreSQL schema/role 初始化与备份恢复** — 目标模块/文件：`auth-service/database-bootstrap.sh`、`project/src/services/README.md`；交付：空卷创建五个独立角色/schema，并按部署 Spec 授予 GORM 自有 schema 迁移所需的数据库级 CREATE；备份/恢复用简洁文档命令说明并按 service ownership 验证；不创建独立 postgres 目录，不提供 PowerShell 运维脚本；覆盖：REQ-02 / AC-02.1, AC-02.2, AC-02.3；REQ-07 / AC-07.3；依赖：T-01。
- [ ] **T-03 Mosquitto TLS/Dynamic Security 初始化** — 目标模块/文件：`device-service/mosquitto.conf`、`device-service/bootstrap-mqtt.sh`；交付：MQTT/TLS listener、broker 持久化、动态安全管理员和两种平台服务的最小 ACL，文档化 Atlas CA/DNS 连接方式；不创建独立 mosquitto/scripts 目录；覆盖：REQ-03 / AC-03.1, AC-03.2, AC-03.3, AC-03.4；依赖：T-01。

### 2. 入口、媒体与基础设施接线

- [ ] **T-04 独立 Web Compose 与 Nginx TLS/API/edge 入口** — 目标模块/文件：`project/src/clients/web/nginx/`、`project/src/clients/web/Dockerfile`、`project/src/clients/web/docker-compose.yaml`、Web `.env.example` 与专属机密；交付：同源 HTTPS、静态 PWA、受保护 API auth_request、logout 例外、edge 路由、可信头覆盖、只发布 Web HTTP/HTTPS、以 external backend network 连接后端且 Web 生命周期可独立管理；覆盖：REQ-01 / AC-01.5；REQ-04 / AC-04.1, AC-04.2, AC-04.3, AC-04.4, AC-04.5；REQ-06 / AC-06.2, AC-06.3；依赖：T-01、T-02。
- [ ] **T-05 MediaMTX/Nginx 逐请求媒体授权集成** — 目标模块/文件：`project/src/services/media-service/mediamtx`、`project/src/services/media-service/mediamtx.yml`、Nginx 媒体路由、media-service 授权入口；交付：WHEP/HLS 请求和子资源逐一通过有效 viewer lease 授权，控制 API/RTSP 由宿主机防火墙隔离，园区 UDP ICE 由宿主机原生监听；不使用 MediaMTX 容器；覆盖：REQ-05 / AC-05.1、AC-05.2、AC-05.3、AC-05.4；依赖：T-01、T-04；开始前重读 `media_service/spec.md`，必要时先同步接口合同和设计再编辑代码。
- [ ] **T-06 后端 Compose 依赖、卷、端口与运行配置整合** — 目标模块/文件：`project/src/services/docker-compose.yaml`、`auth-service` 数据库初始化、`device-service` Mosquitto 配置、`media-service` MediaMTX 配置、五个业务 Dockerfile；交付：后端 Compose 不包含 Web/Nginx；五 schema 角色、required dependency health、非 root 证据卷可写、稳定 backend network、只发布园区 MQTT/TLS、镜像固定 patch；顶层服务目录清一色 `*-service`；覆盖：REQ-01 / AC-01.1–01.4；REQ-02 / AC-02.1–02.3；REQ-03 / AC-03.1–03.4；REQ-04 / AC-04.5；REQ-06 / AC-06.2；依赖：T-01 至 T-05。

### 3. 验证与文档回写

- [ ] **T-07 服务集成测试与部署 smoke test** — 运行/执行：五个服务分别执行 `go test ./...`，Web 执行 `npm run type-check` / `npm test` / `npm run build`，校验 MediaMTX 原生配置与 API readiness，并分别校验两份 Compose、镜像构建、后端健康矩阵、共享网络、独立 Web 生命周期、认证拒绝/成功、设备 edge 路由、MQTT TLS ACL、WHEP/HLS 授权正负路径和宿主机端口/防火墙盘点；覆盖：REQ-01 / AC-01.1–01.5；REQ-02 / AC-02.1–02.2；REQ-03 / AC-03.1–03.3；REQ-04 / AC-04.1–04.5；REQ-05 / AC-05.1–05.4；REQ-06 / AC-06.1–06.3；REQ-07 / AC-07.1, AC-07.2, AC-07.4；依赖：T-01 至 T-06。
- [ ] **T-08 空卷初始化、备份恢复、Atlas 连通与规格回写** — 运行/执行：空卷启动、备份与隔离空卷恢复，Atlas HTTPS/MQTT TLS 实机连通；更新：本任务验收证据、对应 service Spec 与 [全局技术 SPEC](../../Analysis/SPEC.md) / [STRUCTURE](../../Design/STRUCTURE.md) 的实际部署状态；覆盖：REQ-02 / AC-02.3；REQ-03 / AC-03.4；REQ-06 / AC-06.4；REQ-07 / AC-07.3, AC-07.4；依赖：T-07；状态：未开始。

## 验收记录

| AC | 检查方法 | 结果/证据 | 状态 |
|---|---|---|---|
| AC-01.1–01.5 | 分别校验/构建后端与 Web Compose，检查共享网络、健康状态和独立生命周期 | 两份 Compose `config --quiet` 通过；五个 Go 服务镜像和 Web 镜像构建通过。完整栈启动、健康矩阵、MediaMTX readiness 与独立生命周期待运行 | 部分通过 |
| AC-02.1–02.3 | 角色权限、持久化及恢复演练 | 五服务集成测试已在隔离临时 PostgreSQL 上通过；角色 schema 权限、持久卷重建和备份恢复尚未验证 | 部分通过 |
| AC-03.1–03.4 | TLS、ACL、系统 Topic、Atlas 实机 | device-service fake broker 集成测试通过；Mosquitto TLS/Dynamic Security 和 Atlas MQTT 连通待实测 | 部分通过 |
| AC-04.1–04.5 | Nginx 路由、认证和端口审查 | Web 镜像构建与两份 Compose 解析通过；Nginx auth_request/edge 路由运行时和宿主机端口审查待执行 | 部分通过 |
| AC-05.1–05.4 | WHEP/HLS 正负授权与 UDP 网络 | media-service lease/授权集成测试通过 fake MediaMTX；真实 WHEP/HLS、原生 MediaMTX API、UDP ICE/防火墙待实测 | 部分通过 |
| AC-06.1–06.4 | secrets 授权、配置失败、轮换 | 尚未提供 Compose 运行所需服务机密，故没有启动或轮换测试；生产机密不得为验证提交 | 未执行 |
| AC-07.1–07.4 | README 运维命令、手动 smoke、全服务测试、板端证据 | 五服务 unit + integration tests、Web 9/9 Vitest/type-check/build、五服务及 Web Docker build 已通过；实际 HTTPS smoke、备份恢复和 Atlas/园区测试待执行 | 部分通过 |

## 实施偏差与决定

| 日期 | 偏差/新发现 | 规格/设计更新 | 决定人或依据 |
|---|---|---|---|
| 2026-10-19 | 现有 media-service 播放授权 endpoint 需要 JSON body，不能直接由 Nginx auth_request 的无 body 子请求调用。 | T-05 要求先对齐 media-service Spec 和有限输入合同。 | 依据 Nginx auth_request 官方子请求模式与当前源码检查。 |
| 2026-10-19 | Web/Nginx 不属于后端 Compose；Web 客户端源码目录维护独立 Compose，并加入后端创建的 external network。 | T-04、T-06 分别覆盖前后端编排和生命周期验收。 | 用户明确要求部署时前后端分离 |

## 完成摘要

- 已完成任务：无（T-01–T-07 均有依赖或运行验收未完成）。
- 部分通过：Compose 静态解析、五服务和 Web 镜像构建、五服务数据库集成测试、Web 单测/类型检查/生产构建。
- 未执行/未通过检查：真实 Compose 全栈健康、Mosquitto TLS/ACL、MediaMTX WHEP/HLS、角色隔离与恢复、正式 HTTPS/浏览器 smoke、Atlas/园区联通。
- 已同步的权威来源：本次新增部署规格；实现后按实际网络/配置回写全局 SPEC 与 STRUCTURE。
- 完成日期：未完成。
