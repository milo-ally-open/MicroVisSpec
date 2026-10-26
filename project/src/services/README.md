# 后端服务运行说明

MicroVisSpec 后端采用主机原生部署。auth、device、event、media、push 五个 Go 服务各构建为一个 Linux amd64 二进制，由 systemd 管理；PostgreSQL 和 Mosquitto 作为主机服务。Web/Nginx 与整机部署步骤见仓库根目录 [DEPLOY.md](../../../DEPLOY.md)。

## 构建

要求 Go 1.27+。在仓库根目录运行：

```sh
sh project/src/services/build.sh
```

输出在 `project/src/services/bin/linux-amd64/`，每个服务一个可执行文件。该目录已被 Git 忽略。当前 MCP 仅为占位目录，不会生成二进制。

## 服务配置和管理

各 service 目录中的 `*-service.env.example` 是非机密配置模板。复制到 `/etc/microvisspec/` 后配置域名、loopback 端口和 `*_FILE` 密钥文件路径。真实密钥不放在仓库。

| unit | 二进制 | 端口 |
|---|---|---|
| `microvisspec-auth` | `auth-service` | `127.0.0.1:18081` |
| `microvisspec-device` | `device-service` | `127.0.0.1:18082` |
| `microvisspec-event` | `event-service` | `127.0.0.1:18083` |
| `microvisspec-media` | `media-service` | `127.0.0.1:18084` |
| `microvisspec-push` | `push-service` | `127.0.0.1:18085` |

部署时将各 service 的 `microvisspec-*.service` 安装到 `/etc/systemd/system/`。常用操作：

```sh
sudo systemctl status microvisspec-auth
sudo journalctl -u microvisspec-auth -n 100 --no-pager
sudo systemctl restart microvisspec-auth
sudo systemctl stop microvisspec-auth
curl -fsS http://127.0.0.1:18081/health/ready
```

对应服务名和端口替换后可检查其余服务。`/health/ready` 检查必需依赖；异常时先查看对应 unit 的 journal。

## 初始化、数据和网络

- PostgreSQL 首次建库后，以 `postgres` 系统用户执行 `auth-service/database-bootstrap.sh`。该脚本创建五个角色和 schema，可重复运行。
- Mosquitto TLS 配置模板与可重复的 ACL 初始化脚本位于 `device-service/`；Dynamic Security 插件路径按发行版校准。
- MediaMTX 的二进制及运行配置归 `media-service/`，通过其独立 systemd unit 管理。
- Go listener 仅绑定 loopback。主机防火墙只开放 Web HTTPS、园区 MQTT/TLS 和批准的 WebRTC ICE 端口。
- PostgreSQL、Mosquitto 与 event evidence 是持久数据。备份恢复及完整部署顺序见 [DEPLOY.md](../../../DEPLOY.md)；不要通过删除数据目录释放空间。
