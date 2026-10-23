# Yunshao 部署

本目录保存后端 Compose 编排和各 `*-service` 的部署文件。Web/Nginx 由 `project/src/clients/web/docker-compose.yaml` 单独管理。需要 Docker Compose v2、部署机 TLS 证书和各服务所需的 `.secret-*` 文件；机密由运维或机密管理系统提供，按对应 Compose 文件的映射放入 owner 目录。不要提交机密文件。

## 首次启动

1. 复制本目录 `.env.example` 为 `.env`，设置 HTTPS 来源、Atlas 可达的 MQTT 绑定地址和共享网络名 `BACKEND_NETWORK`。首次网页接入还需将 `ATLAS_SSH_ALLOWED_CIDRS` 限定为允许服务端 SSH 访问的设备网段。
   `MQTT_ADVERTISED_BROKER_URL` 是激活响应交给 Atlas 的 TLS 地址；它与容器内部使用的 `ssl://mosquitto:8883` 分开配置，并且必须路由到该 broker TLS listener。设备端必须信任 broker 证书 CA。
2. 准备各 `*-service` 所需的 `.secret-*` 文件，并在 `device-service` 放入签发 Web HTTPS 证书的公开 CA PEM：`.secret-atlas-activation-ca-certificate.pem`。不要放 Web 叶子证书或 CA 私钥。Atlas 首次接入时由设备服务通过受限 SSH 自动部署 agent 和该 CA；设备需预装 Atlas 驱动/CANN、启用 SSH，并允许部署容器访问 TCP/22。另在 `media-service` 准备含运行凭据的本地 `mediamtx.yml`。可提交的示例为 `media-service/mediamtx.yml.example`。
3. 下载 MediaMTX 官方单文件程序放入 `media-service`，核对官方 release checksum。在 `media-service` 目录先运行 `./mediamtx --validate-conf=mediamtx.yml`，再直接运行 `./mediamtx mediamtx.yml`；保持该进程运行。
4. 在 `project/src/services` 校验并启动后端：

```sh
docker compose --env-file .env config --quiet
docker compose --env-file .env up --build -d
docker compose --env-file .env ps
```

后端 Compose 会创建默认名为 `yunshao_backend` 的共享网络。然后在 `project/src/clients/web` 复制 `.env.example` 为 `.env`，确认 `BACKEND_NETWORK` 与后端 `.env` 一致，并准备该目录所需的 Nginx TLS/header `.secret-*` 文件，再独立校验并启动 Web：

```sh
docker compose config --quiet
docker compose up --build -d
docker compose ps
```

Web Compose 只管理 Nginx/Web/PWA；停止、重建或升级它不会操作后端 Compose。Web 健康状态表示静态入口可用，后端 readiness 仍单独查看。Windows 开发机可在命令提示符中执行 `docker compose` 命令；MediaMTX 部署二进制应与运行主机系统和架构一致。仓库不提供 PowerShell 运维脚本或服务启动包装脚本。

## 检查与日志

在 `project/src/services` 检查后端：

```sh
docker compose --env-file .env ps
docker compose --env-file .env logs --tail 100 auth-service device-service event-service media-service push-service
docker compose --env-file .env logs -f
```

在 `project/src/clients/web` 检查前端入口：

```sh
docker compose ps
docker compose logs --tail 100 web
docker compose logs -f web
```

从部署主机执行浏览器入口 smoke 检查：

```sh
curl --fail --show-error https://your-configured-host/
curl --fail --show-error https://your-configured-host/api/v1/push/vapid-public-key
```

后端 `docker compose ps` 中 required 容器应为 healthy；Web Compose healthy 只证明 Nginx 静态入口可用。手动检查时再确认未登录的 `/api/v1/auth/me`、`/media/<camera-id>/index.m3u8` 与 WHEP 请求被拒绝；用已有测试账号单独验证登录和退出。Atlas 连接需使用部署 CA 验证 MQTT/TLS 与 edge HTTPS。

## PostgreSQL 备份与恢复

备份写到仓库之外并按组织要求加密保存：

```sh
docker compose --env-file .env exec -T postgres sh -eu -c 'export PGPASSWORD="$(cat /run/secrets/postgres_admin_password)"; pg_dump --format=custom --no-acl --username="$POSTGRES_USER" --dbname="$POSTGRES_DB"' > /secure/backup/location/yunshao.dump
```

恢复前停止业务写入并确认目标数据库；恢复会覆盖同名对象：

```sh
cat /secure/backup/location/yunshao.dump | docker compose --env-file .env exec -T postgres sh -eu -c 'export PGPASSWORD="$(cat /run/secrets/postgres_admin_password)"; pg_restore --clean --if-exists --exit-on-error --username="$POSTGRES_USER" --dbname="$POSTGRES_DB"'
```

数据库备份不含截图证据、service 机密或 Mosquitto 持久卷。常规停止会保留数据库、broker 和证据卷；不要用 `down -v` 清理运行数据。

## 网络边界

Web Compose 发布 Nginx HTTP/HTTPS；后端 Compose 发布 Mosquitto TLS。两者共享 `BACKEND_NETWORK`，MediaMTX 是 `media-service` 中直接运行的宿主机进程。宿主机防火墙只允许 Compose 容器访问 MediaMTX API/RTSP/HLS/WHEP 端口，并按园区网段开放 UDP 8189；数据库、Go 服务端口及 Mosquitto 管理端口不对宿主机发布。
