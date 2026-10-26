# 提案：MicroVisSpec 原生轻量部署

> 状态：实现中<br>
> 范围：平台部署<br>
> 关联：[行为规格](spec.md)、[技术设计](design.md)、[实施任务](tasks.md)、[全局技术规格](../../Analysis/SPEC.md)、[系统结构](../../Design/STRUCTURE.md)

## 背景与目标

当前五个 Go 微服务通过 Docker 镜像运行，Web/PWA 也需构建并运行 Nginx 镜像。用户要求移除容器运行依赖：每个 Go 服务只交付一个二进制，前端以静态文件部署，并释放本机 Docker 磁盘空间。

目标是在单台 Ubuntu/Debian Linux amd64 主机上，用 systemd 管理五个 Go 二进制；Vue/PWA 静态构建由主机 Nginx 提供；PostgreSQL、Mosquitto 与 MediaMTX 原生运行。保留现有服务边界、API、安全规则和持久数据合同。

## 范围

### 包含

- 五个已实现 Go 服务的 Linux amd64 构建、systemd unit、服务配置和日志查看。
- Web 静态构建、Nginx HTTPS/API/auth_request/edge/media 代理配置。
- PostgreSQL 五角色/schema、Mosquitto TLS/Dynamic Security、MediaMTX 原生运行。
- 主机网络暴露边界、备份恢复与端到端部署说明。
- 删除仓库中的 Compose 和 Dockerfile 部署文件；清理本机本项目已停止容器与无引用镜像。

### 不包含/保持不变

- MCP 服务仍未实现，不构建、不部署。
- 不改变 API、会话/CSRF、设备身份、MQTT topic、事件行为、媒体授权及数据保留合同。
- 不引入 Kubernetes、多机 HA、容器运行时；不声称 Atlas 推理性能验收已完成。
- 不删除 PostgreSQL、Mosquitto、evidence 等持久化数据。

## 部署约束

- 首发部署平台为 Linux amd64。Go 服务监听 loopback：auth `18081`、device `18082`、event `18083`、media `18084`、push `18085`。
- Nginx 发布 Web/API HTTPS；Mosquitto 发布园区 MQTT/TLS；媒体 ICE 端口按园区网络开放。Go、PostgreSQL、MediaMTX 控制/RTSP/HLS/WHEP 端口不得向外部网络公开。
- 服务敏感配置仍用 `*_FILE`；部署机密存 `/etc/microvisspec` 并限制权限。仓库样例不得包含密钥。
- PostgreSQL/Mosquitto 保留系统数据目录；事件证据位于 `/var/lib/microvisspec/evidence`。本次清理不得删除持久数据。

## 方案与取舍

采用静态 Go 二进制 + systemd、Nginx + 静态 Web、系统包 PostgreSQL/Mosquitto 和 MediaMTX 单文件程序。这样不需要 Docker Engine、镜像和编排网络，同时保留独立服务生命周期。继续 Compose 不符合用户的去 Docker 化要求；Kubernetes 超出单机轻量部署范围；合并服务会破坏现有责任边界。

## 风险与控制

- 原生服务共享主机网络：Go 绑定 loopback，由 Nginx 代理；主机防火墙仅开放必要入口。
- systemd 环境文件错误暴露密钥：配置只记录路径；密钥文件由 root 持有并按服务组授予只读权限，不进入 Git。
- 系统包升级及数据恢复由运维负责：文档提供状态检查和备份/恢复步骤，并要求在生产使用前演练。
- 容器卷中的历史数据需要显式迁出；清理本次只删项目容器及无引用镜像，保留所有 Docker 卷。

## 变更记录

| 日期 | 内容 |
|---|---|
| 2026-10-26 | 将首发部署从 Compose 改为 Linux 主机原生部署；五个 Go 二进制，前端静态化。 |
