# 部署文档入口

完整的 MicroVisSpec 原生部署步骤已经统一到仓库根目录的 [DEPLOY.md](../DEPLOY.md)，包括 Go 二进制构建、Web 静态发布、Nginx、PostgreSQL、Mosquitto、MediaMTX、systemd、状态检查与备份恢复。

Atlas 200I DK A2 的 agent、推理模型、设备激活和视频端到端操作见[板端工程 README](../project/src/device_sides/atlas200idka2/README.md)。

本项目运行部署不依赖 Docker Compose。历史部署记录与旧 Docker 指令不再作为当前操作步骤。
