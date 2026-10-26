# 实施任务：原生轻量部署

> 任务追踪：[spec.md](spec.md)；执行命令和运维步骤见 [DEPLOY.md](../../../../DEPLOY.md)。

## 任务清单

- [x] **T-01 五个 Go 二进制与 systemd 单元**：在各 `*-service` 中保留配置与 Go 模块，增加 Linux amd64 build.sh、五个独立 unit 和环境样例；确认 unit 依赖并不形成循环。覆盖 REQ-01 AC-01.1–01.2、REQ-06；每个服务改动前回读其 service Spec。
- [x] **T-02 PostgreSQL 原生初始化与数据操作**：适配 `auth-service/database-bootstrap.sh` 为 PostgreSQL OS 用户执行，保持五 schema/角色及权限；文档覆盖首次创建、备份与恢复。覆盖 REQ-02。
- [x] **T-03 Mosquitto 原生 TLS 与 ACL**：把配置路径改为系统路径，bootstrap 从受限文件读取管理员/服务身份配置，保持幂等 ACL；保留 device-service owner 目录。覆盖 REQ-03。
- [x] **T-04 Web 静态发布与 Nginx 原生入口**：更新 Nginx 上游至 loopback 端口、移除 Docker host gateway 和容器 secret 路径；通过静态 dist + 系统 Nginx 部署。覆盖 REQ-01 AC-01.3–01.5、REQ-04。
- [x] **T-05 MediaMTX 原生进程与受保护媒体入口**：提供 systemd unit，控制 API/RTSP/HLS/WHEP 端口仅供本机访问，并保留园区 WebRTC UDP；部署主机媒体链路仍待验收。覆盖 REQ-05。
- [x] **T-06 密钥和运行配置**：各服务 env 样例只含非机密值和 `*_FILE` 路径；部署文档说明 `/etc/microvisspec` 文件权限和密钥生成。覆盖 REQ-06。
- [ ] **T-07 文档与部署验证**：更新 DEPLOY.md、README、STRUCTURE、全局 SPEC 和 service README；删除 Compose/Dockerfile 部署入口；Go build 与 Web production build 已通过。Nginx 语法检查及目标 Linux 主机 smoke 尚未执行。覆盖 REQ-07 AC-07.1–07.4。
- [x] **T-08 本机 Docker 清理与磁盘记录**：移除确认属于本项目的 22 个已退出容器和未引用项目镜像；不删除数据卷或构建缓存，记录清理前后占用。覆盖 REQ-07 AC-07.5。
- [ ] **T-09 默认分支交付**：确认改动、生成本地 commit，并推送至项目远程仓库默认 `main`；不将运行时密钥或生成的 Go 二进制加入 Git。

## 验收记录

| 检查 | 结果 |
|---|---|
| 五个 Linux amd64 Go build | 通过；构建生成五个独立二进制，未纳入 Git |
| Web type-check + production build | 通过；使用 Node.js 24/npm 完成 `npm ci` 与 `npm run build` |
| Nginx 配置检查 | 待执行（需目标 Linux Nginx） |
| 原生 PostgreSQL/Mosquitto/systemd 启动与恢复 | 待部署主机执行 |
| HTTPS/认证/edge/媒体端到端 | 待部署主机和 Atlas 验证 |
| 本机 Docker 清理差值 | 已完成；容器 22→0，镜像占用 2.167 GB→1.743 GB，约释放 424 MB；28 个数据卷（922.9 MB）和 3.846 GB 构建缓存保留 |

## 偏差与未完成项

- Linux 服务实际启动、证书可达性、Atlas 连接、视频播放及数据库恢复必须由目标主机验收；不能由本机 Go/NPM 构建替代。
- Docker Desktop 的 `docker system df` 按共享镜像层计量；本次只移除了项目容器及项目专属镜像层，未清理共享基础镜像、数据卷或构建缓存。
- MCP 仍未实现，不纳入二进制或服务清单。
