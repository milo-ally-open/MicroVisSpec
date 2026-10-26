# MicroVisSpec 部署指南

本文介绍无 Docker 的单机部署：五个 Go 服务各自一个二进制，Web/PWA 使用静态文件，Nginx、PostgreSQL、Mosquitto、MediaMTX 在 Linux 主机原生运行。Atlas 端模型与 agent 的安装步骤见 [Atlas 工程 README](project/src/device_sides/atlas200idka2/README.md)。

## 1. 准备主机

部署目标是 Ubuntu/Debian Linux amd64。构建机需要 Go 1.27+、Node.js 24+ 和 npm；运行机安装 Nginx、PostgreSQL、Mosquitto 和 MQTT 客户端工具。Docker 不参与构建或运行。

```sh
sudo apt update
sudo apt install -y nginx postgresql mosquitto mosquitto-clients openssl ca-certificates curl unzip
```

启用系统服务：

```sh
sudo systemctl enable postgresql mosquitto nginx
sudo systemctl enable --now postgresql mosquitto
```

确认系统 Mosquitto 安装了 Dynamic Security 插件。Ubuntu/Debian 的常见位置为 `/usr/lib/x86_64-linux-gnu/mosquitto_dynamic_security.so`，以本机安装文件为准；插件由 Mosquitto 官方发行版提供。[Mosquitto Dynamic Security 安装说明](https://mosquitto.org/documentation/dynamic-security/)

## 2. 获取源码并构建

```sh
git clone https://github.com/milo-ally-open/MicroVisSpec.git
cd MicroVisSpec
```

在 Linux amd64 构建五个 Go 可执行文件：

```sh
sh project/src/services/build.sh
```

构建 Web/PWA 静态文件：

```sh
cd project/src/clients/web
npm ci
npm run build
cd ../../../..
```

Go 输出位于 `project/src/services/bin/linux-amd64/`，Web 输出位于 `project/src/clients/web/dist/`。两个目录都不提交到 Git。

## 3. 创建运行目录与服务用户

```sh
sudo groupadd --system microvisspec
sudo useradd --system --gid microvisspec --home-dir /var/lib/microvisspec --shell /usr/sbin/nologin microvisspec
sudo install -d -o root -g root -m 0755 /etc/microvisspec
sudo install -d -o microvisspec -g microvisspec -m 0750 /var/lib/microvisspec/evidence
sudo install -d -o root -g root -m 0755 /usr/local/bin/microvisspec
sudo install -d -o root -g root -m 0755 /var/www/microvisspec
sudo install -d -o root -g root -m 0755 /opt/microvisspec /opt/microvisspec/device-service
sudo install -d -o root -g root -m 0755 /etc/nginx/snippets
```

安装五个服务二进制与静态站点：

```sh
sudo install -m 0755 project/src/services/bin/linux-amd64/auth-service /usr/local/bin/microvisspec/auth-service
sudo install -m 0755 project/src/services/bin/linux-amd64/device-service /usr/local/bin/microvisspec/device-service
sudo install -m 0755 project/src/services/bin/linux-amd64/event-service /usr/local/bin/microvisspec/event-service
sudo install -m 0755 project/src/services/bin/linux-amd64/media-service /usr/local/bin/microvisspec/media-service
sudo install -m 0755 project/src/services/bin/linux-amd64/push-service /usr/local/bin/microvisspec/push-service
sudo cp -a project/src/clients/web/dist/. /var/www/microvisspec/
```

## 4. 配置 PostgreSQL 和服务密钥

创建应用数据库：

```sh
sudo -u postgres createdb microvisspec
```

为 auth、device、event、media、push 分别生成数据库密码文件：

```sh
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/auth-database-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/device-database-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/event-database-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/media-database-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/push-database-password'
```

数据库密码文件供首次初始化读取；URL 文件供对应 Go 服务读取。URL 格式如下，将 `<密码>` 替换为该服务生成的密码：

```text
postgres://auth_service:<密码>@127.0.0.1:5432/microvisspec?sslmode=disable
```

为每个服务分别建立名为 `auth-database-url`、`device-database-url`、`event-database-url`、`media-database-url`、`push-database-url` 的 URL 文件，并将 URL 里的角色和密码替换为对应服务值。服务环境样例的 `DATABASE_URL_FILE` 已指向这些路径。

例如可用 `sudoedit /etc/microvisspec/auth-database-url` 创建第一个 URL 文件；其余四个按相同格式创建，文件内只放一行连接 URL，不加引号。

分别参考 `project/src/services/<service>/*.env.example` 创建 `/etc/microvisspec/<service>.env`，并编辑域名、CA 文件路径及其他非机密配置。例：

```sh
sudo install -o root -g microvisspec -m 0640 project/src/services/auth-service/auth-service.env.example /etc/microvisspec/auth-service.env
sudo install -o root -g microvisspec -m 0640 project/src/services/device-service/device-service.env.example /etc/microvisspec/device-service.env
sudo install -o root -g microvisspec -m 0640 project/src/services/event-service/event-service.env.example /etc/microvisspec/event-service.env
sudo install -o root -g microvisspec -m 0640 project/src/services/media-service/media-service.env.example /etc/microvisspec/media-service.env
sudo install -o root -g microvisspec -m 0640 project/src/services/push-service/push-service.env.example /etc/microvisspec/push-service.env
sudoedit /etc/microvisspec/auth-service.env /etc/microvisspec/device-service.env /etc/microvisspec/event-service.env /etc/microvisspec/media-service.env /etc/microvisspec/push-service.env
```

服务用到的内部令牌、数据库密码、MQTT 服务密码、加密/签名密钥和 VAPID 私钥需在部署主机生成。以随机 256 位 hex 值为例：

```sh
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/auth-internal-service-token'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/device-internal-service-token'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/device-media-service-token'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/auth-media-service-token'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/auth-push-service-token'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/event-push-service-token'
sudo sh -c 'umask 077; openssl rand -base64 32 > /etc/microvisspec/media-source-encryption-key'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/media-nginx-internal-token'
sudo sh -c 'umask 077; openssl rand -base64 48 > /etc/microvisspec/event-cursor-signing-key'
sudo sh -c 'umask 077; openssl rand -base64 32 > /etc/microvisspec/push-encryption-key'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/mqtt-admin-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/mqtt-device-service-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/mqtt-event-service-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/mediamtx-api-password'
sudo sh -c 'umask 077; openssl rand -hex 32 > /etc/microvisspec/mediamtx-reader-password'
```

Push VAPID 密钥可用 `web-push` 生成并暂存到权限受限的临时文件，再把公钥和私钥分别写入环境样例指定的文件：

```sh
umask 077
npx --yes web-push generate-vapid-keys --json > /tmp/microvisspec-vapid.json
sudoedit /etc/microvisspec/push-vapid-public-key /etc/microvisspec/push-vapid-private-key
```

将 JSON 中的 `publicKey`、`privateKey` 分别复制到对应文件，完成后安全删除 `/tmp/microvisspec-vapid.json`。Web 与 MQTT TLS 证书应由正式 CA 签发；内网试点可用 mkcert 临时生成。证书 SAN 必须覆盖浏览器与 Atlas 使用的主机名，私钥不可复制给客户端。

mkcert 生成 Web 证书示例（在证书生成机执行；替换为实际域名）：

```sh
mkcert -cert-file web-tls-certificate.pem -key-file web-tls-private-key.pem your-domain.example
sudo install -o root -g www-data -m 0644 web-tls-certificate.pem /etc/microvisspec/web-tls-certificate.pem
sudo install -o root -g www-data -m 0640 web-tls-private-key.pem /etc/microvisspec/web-tls-private-key.pem
```

mkcert 仅适合试点；访问端也必须信任该 mkcert 根证书。MQTT 证书同样需要覆盖 broker 主机名，并将对应 CA 证书提供给 Atlas 与五个 Go 服务。

数据库 URL、Go 密钥、MQTT 管理和服务密码、MediaMTX 密码文件生成时均使用 `umask 077`。数据库密码需让 PostgreSQL 初始化脚本可读；其余 Go 密钥需让 `microvisspec` 组可读。生成后按以下权限配置：

```sh
sudo chown root:postgres /etc/microvisspec/auth-database-password /etc/microvisspec/device-database-password /etc/microvisspec/event-database-password /etc/microvisspec/media-database-password /etc/microvisspec/push-database-password
sudo chmod 0640 /etc/microvisspec/*-database-password
sudo chown root:microvisspec /etc/microvisspec/*-database-url /etc/microvisspec/*-service-token /etc/microvisspec/device-media-service-token /etc/microvisspec/auth-media-service-token /etc/microvisspec/media-source-encryption-key /etc/microvisspec/media-nginx-internal-token /etc/microvisspec/event-cursor-signing-key /etc/microvisspec/push-encryption-key /etc/microvisspec/push-vapid-public-key /etc/microvisspec/push-vapid-private-key /etc/microvisspec/mqtt-admin-password /etc/microvisspec/mqtt-device-service-password /etc/microvisspec/mqtt-event-service-password /etc/microvisspec/mediamtx-api-password /etc/microvisspec/mediamtx-reader-password
sudo chmod 0640 /etc/microvisspec/*-database-url /etc/microvisspec/*-service-token /etc/microvisspec/device-media-service-token /etc/microvisspec/auth-media-service-token /etc/microvisspec/media-source-encryption-key /etc/microvisspec/media-nginx-internal-token /etc/microvisspec/event-cursor-signing-key /etc/microvisspec/push-encryption-key /etc/microvisspec/push-vapid-public-key /etc/microvisspec/push-vapid-private-key /etc/microvisspec/mqtt-admin-password /etc/microvisspec/mqtt-device-service-password /etc/microvisspec/mqtt-event-service-password /etc/microvisspec/mediamtx-api-password /etc/microvisspec/mediamtx-reader-password
sudo chown root:microvisspec /etc/microvisspec/auth-service.env /etc/microvisspec/device-service.env /etc/microvisspec/event-service.env /etc/microvisspec/media-service.env /etc/microvisspec/push-service.env
sudo chmod 0640 /etc/microvisspec/*-service.env
```

MQTT CA 证书应对 `microvisspec` 组可读；MQTT TLS 私钥和管理员密码副本设为 `root:mosquitto`、`0640`。Web TLS 私钥和 Nginx header include 设为 `root:www-data`、`0640`；MediaMTX YAML 设为 `root:microvisspec`、`0640`。配置目录为 `root:root`、`0755`。不要将密钥写入环境文件或仓库。

创建 Nginx 所需的 header include；命令只把密钥写入文件，不打印密钥：

```sh
sudo sh -c 'printf "proxy_set_header X-Internal-Service-Token \"%s\";\n" "$(cat /etc/microvisspec/auth-internal-service-token)" > /etc/microvisspec/nginx-auth-header.conf'
sudo sh -c 'printf "proxy_set_header X-Internal-Service-Token \"%s\";\n" "$(cat /etc/microvisspec/media-nginx-internal-token)" > /etc/microvisspec/nginx-media-header.conf'
sudo sh -c 'printf "proxy_set_header Authorization \"Basic %s\";\n" "$(printf "nginx-media-reader:%s" "$(cat /etc/microvisspec/mediamtx-reader-password)" | base64 | tr -d "\n")" > /etc/microvisspec/nginx-mediamtx-reader-header.conf'
sudo chown root:www-data /etc/microvisspec/nginx-auth-header.conf /etc/microvisspec/nginx-media-header.conf /etc/microvisspec/nginx-mediamtx-reader-header.conf
sudo chmod 0640 /etc/microvisspec/nginx-auth-header.conf /etc/microvisspec/nginx-media-header.conf /etc/microvisspec/nginx-mediamtx-reader-header.conf
```

应用 env 文件只含配置值与 `*_FILE` 路径，不放密钥内容。若启用网页 SSH 初始化 Atlas，还需将按 Atlas README 构建的设备 bootstrap 包安装到 `/opt/microvisspec/atlas200idka2-bootstrap.tar.gz`。

运行一次数据库初始化。确保 `/etc/microvisspec` 中的五个数据库密码文件对 postgres 用户可读：

```sh
sudo -u postgres env PGDATABASE=microvisspec \
  AUTH_DB_PASSWORD_FILE=/etc/microvisspec/auth-database-password \
  DEVICE_DB_PASSWORD_FILE=/etc/microvisspec/device-database-password \
  EVENT_DB_PASSWORD_FILE=/etc/microvisspec/event-database-password \
  MEDIA_DB_PASSWORD_FILE=/etc/microvisspec/media-database-password \
  PUSH_DB_PASSWORD_FILE=/etc/microvisspec/push-database-password \
  sh project/src/services/auth-service/database-bootstrap.sh
```

## 5. 配置 Mosquitto 与 MediaMTX

把 Mosquitto 配置安装到系统 conf.d，并确认插件 `.so` 路径、MQTT TLS 证书和私钥路径正确：

```sh
sudo install -o root -g mosquitto -m 0640 /etc/microvisspec/mqtt-admin-password /etc/microvisspec/mqtt-admin-password-mosquitto
sudo install -o root -g mosquitto -m 0644 mqtt-tls-certificate.pem /etc/microvisspec/mqtt-tls-certificate.pem
sudo install -o root -g mosquitto -m 0640 mqtt-tls-private-key.pem /etc/microvisspec/mqtt-tls-private-key.pem
sudo install -o root -g microvisspec -m 0644 mqtt-ca-certificate.pem /etc/microvisspec/mqtt-ca-certificate.pem
sudo install -o root -g microvisspec -m 0644 atlas-activation-ca-certificate.pem /etc/microvisspec/atlas-activation-ca-certificate.pem
sudo install -o root -g root -m 0644 project/src/services/device-service/mosquitto.conf /etc/mosquitto/conf.d/microvisspec.conf
sudoedit /etc/mosquitto/conf.d/microvisspec.conf
```

Mosquitto 配置中的 Dynamic Security plugin 路径应与主机实际安装位置一致。用 `sudoedit /etc/microvisspec/mosquitto-ctrl.options` 创建仅 root 可读的选项文件，写入 broker 主机名、端口、CA 路径、管理员账号和密码，例如 `-h mqtt.your-domain.example`、`-p 8883`、`--cafile /etc/microvisspec/mqtt-ca-certificate.pem`、`-u admin`、`-P <管理员密码>`。

设备/事件系统身份与 ACL 初始化脚本可重复执行：

```sh
sudo install -m 0755 project/src/services/device-service/bootstrap-mqtt.sh /opt/microvisspec/device-service/bootstrap-mqtt.sh
sudo install -m 0644 project/src/services/device-service/microvisspec-mosquitto-bootstrap.service /etc/systemd/system/
sudo chmod 0600 /etc/microvisspec/mosquitto-ctrl.options
sudo systemctl daemon-reload
```

从 [MediaMTX 官方发布页](https://github.com/bluenviron/mediamtx/releases) 选择 Linux amd64 版本并下载对应压缩包。解压其中的 `mediamtx` 单文件，安装到 `/opt/microvisspec/mediamtx`。将 `project/src/services/media-service/mediamtx.yml.example` 复制到 `/etc/microvisspec/mediamtx.yml`，填入来源流及 API/读者凭据。API 监听只供本机访问，WebRTC ICE 端口仅按园区网络开放。

```sh
sudo install -m 0755 mediamtx /opt/microvisspec/mediamtx
sudo install -m 0644 project/src/services/media-service/mediamtx.yml.example /etc/microvisspec/mediamtx.yml
sudo install -m 0644 project/src/services/media-service/mediamtx.service /etc/systemd/system/mediamtx.service
sudoedit /etc/microvisspec/mediamtx.yml
```

编辑 YAML 中的 `__MEDIAMTX_API_PASSWORD__`、`__MEDIAMTX_READER_PASSWORD__`、`__PUBLIC_ORIGIN__` 和 `__WEBRTC_ADDITIONAL_HOSTS__` 占位值；最后一项填可被观看终端访问的主机名列表，例如 `["your-domain.example"]`。MediaMTX 配置包含服务密码，权限设为 `root:microvisspec`、`0640`。

## 6. 安装 Nginx 与 systemd units

Nginx 配置将五个服务代理到 loopback 端口 `18081`–`18085`。先准备 Web 证书，再安装配置和静态资源路径：

```sh
sudo install -m 0644 project/src/clients/web/nginx/nginx.conf /etc/nginx/nginx.conf
sudo install -m 0644 project/src/clients/web/nginx/snippets/proxy-common.conf /etc/nginx/snippets/proxy-common.conf
sudo nginx -t
```

确认 `/etc/microvisspec/nginx-auth-header.conf`、`nginx-media-header.conf`、`nginx-mediamtx-reader-header.conf` 均存在且属主为 `root:www-data`、权限为 `0640`；Web TLS 私钥也须允许 `www-data` 读取。正式环境应使用受信任的 TLS 证书。

安装五个 Go 服务 unit：

```sh
sudo install -m 0644 project/src/services/auth-service/microvisspec-auth.service /etc/systemd/system/
sudo install -m 0644 project/src/services/device-service/microvisspec-device.service /etc/systemd/system/
sudo install -m 0644 project/src/services/event-service/microvisspec-event.service /etc/systemd/system/
sudo install -m 0644 project/src/services/media-service/microvisspec-media.service /etc/systemd/system/
sudo install -m 0644 project/src/services/push-service/microvisspec-push.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo nginx -t
sudo systemctl enable --now nginx
```

先启动 MediaMTX、Mosquitto ACL 初始化、再启动 Go 服务。Push 与 auth 先启动，device/media 随后，event 最后：

```sh
sudo systemctl enable --now mediamtx.service
sudo systemctl restart mosquitto.service
sudo systemctl start microvisspec-mosquitto-bootstrap.service
sudo systemctl enable --now microvisspec-push.service microvisspec-auth.service
sudo systemctl enable --now microvisspec-device.service microvisspec-media.service
sudo systemctl enable --now microvisspec-event.service
sudo nginx -t && sudo systemctl reload nginx
```

## 7. 检查运行状态

```sh
systemctl --no-pager --full status microvisspec-auth microvisspec-device microvisspec-event microvisspec-media microvisspec-push
journalctl -u microvisspec-auth -n 100 --no-pager
curl -fsS http://127.0.0.1:18081/health/ready
curl -fsS http://127.0.0.1:18082/health/ready
curl -fsS http://127.0.0.1:18083/health/ready
curl -fsS http://127.0.0.1:18084/health/ready
curl -fsS http://127.0.0.1:18085/health/ready
```

从浏览器打开配置好的 HTTPS 域名，确认页面加载、登录/退出、未登录 API 拒绝、设备列表、授权监控及告警页面。播放测试流可按[视频推流 SOP](project/src/device_sides/atlas200idka2/README.md)准备；无摄像头时使用仓库 `video/` 测试视频。部署完成后再接入 Atlas，并按该 README 检查激活、MQTT、实时画面和事件链路。

## 8. 升级、停止与备份

升级 Go 服务时重新执行 build.sh、备份旧二进制，再替换所需文件并重启对应 unit。Web 更新时重新执行 `npm run build`，替换 `/var/www/microvisspec` 内容、运行 `nginx -t` 后 reload。不要因更新某个 Go 服务而重启数据库或其他服务。

```sh
sudo systemctl restart microvisspec-auth
sudo systemctl stop microvisspec-event microvisspec-media microvisspec-device microvisspec-push microvisspec-auth
sudo -u postgres pg_dump --format=custom microvisspec > /secure/backup/location/microvisspec.dump
sudo -u postgres pg_restore --clean --if-exists --exit-on-error --dbname=microvisspec /secure/backup/location/microvisspec.dump
```

备份 PostgreSQL 和必要的 Mosquitto/MediaMTX 配置；备份位置应在部署机之外。不要删除 `/var/lib/postgresql`、`/var/lib/mosquitto` 或 `/var/lib/microvisspec/evidence` 来释放空间。历史 Docker 数据卷如需迁移，先单独导出并在原生服务恢复验证后再决定是否清理。
