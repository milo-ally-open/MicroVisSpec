# 云哨从零部署指南

本文从空白 Linux 部署机和已刷好系统的 Atlas 200I DK A2 开始，部署云端、Web/PWA、媒体服务和端侧课堂行为检测，并完成浏览器端到端验证。命令示例使用 Linux/WSL shell；Windows 操作使用命令提示符，不使用 PowerShell。

默认示例网络与当前联调环境一致：云端主机 `192.168.0.1`，Atlas 开发板 `192.168.0.2`，Web HTTPS 端口 `19080`。若部署网络不同，请整体替换云端地址并按实际修改板卡 IP。`.env`、`.secret-*`、私钥、激活票据和数据库备份只保存在部署机或设备上，不要提交到 Git。

## 1. 部署拓扑与前置条件

部署包含四个单元：

| 单元 | 位置 | 启动方式 |
| --- | --- | --- |
| auth、device、event、media、push 服务，PostgreSQL、Mosquitto | `project/src/services` | 独立 Docker Compose |
| Web 与 Nginx | `project/src/clients/web` | 独立 Docker Compose，加入后端共享网络 |
| MediaMTX | `project/src/services/media-service` | 官方单文件程序 `mediamtx` 和本地 `mediamtx.yml`，由主机进程管理 |
| agent、课堂检测 runtime、模型插件 | `project/src/device_sides/atlas200idka2` | Atlas 上的 systemd 服务 |

需要准备：

- 安装 Docker Engine/Docker Desktop 与 Compose v2 的 Linux 主机；使用 Docker Desktop 时，确认 Docker 能访问 WSL 的仓库目录。
- 可访问云端主机的 Atlas 200I DK A2，已安装与板卡固件匹配的 Ascend CANN、驱动和运行时。
- 云端主机可访问 `192.168.0.2:8554/TCP`；Atlas 可访问 `192.168.0.1:8883/TCP` 和 `192.168.0.1:19080/TCP`。
- Linux/WSL 上的 `git`、`curl`、`openssl`、`mkcert`、`jq`、`base64`、`sha256sum`、`tar`；生成 Web Push 密钥还需要 Node.js/npm。
- Windows 浏览器和 `ffmpeg`、`ffplay`。测试视频在仓库 `video/classroom_behavior.mp4`。

生产环境应使用组织认可的 DNS、HTTPS 证书和 MQTT CA。下文使用 mkcert 生成隔离开发/试点证书；它不是公网生产证书。浏览器、Atlas 和云端都必须信任同一 CA。

### 网络端口

| 端口 | 方向 | 用途 |
| --- | --- | --- |
| `19080/TCP` | 浏览器 → 云端 | 当前联调 Web HTTPS 端口；直接访问 `https://192.168.0.1:19080`。 |
| `8883/TCP` | Atlas → 云端 | Mosquitto MQTT/TLS。 |
| `8554/TCP` | 云端 → Atlas | MediaMTX 按需拉取 Atlas 处理后的 RTSP。 |
| `8189/UDP` | 浏览器 → 云端 | WebRTC 媒体；只对需要观看的内网/VPN 网段开放。 |
| `5000/UDP` | Windows → Atlas | 本指南的 FFmpeg 测试输入；实际部署不需要此端口。 |

MediaMTX API `9997`、HLS `8888` 和 WebRTC 信令 `8889` 由本机 Docker/Nginx 使用，不要开放给公网。PostgreSQL `5432`、Go 服务内部端口和 MQTT 管理接口也不要映射到公网。

## 2. 获取代码

在部署机或 WSL 中执行：

```sh
git clone --branch dev https://atomgit.com/milo-ally-stu/Yunshao.git
cd Yunshao
```

`main` 与 `dev` 当前保持同步。后续命令以仓库根目录 `Yunshao/` 为起点，除非代码块写明了 `cd`。

## 3. 配置部署地址与 TLS

复制两个 Compose 项目的环境模板：

```sh
cp project/src/services/.env.example project/src/services/.env
cp project/src/clients/web/.env.example project/src/clients/web/.env
```

编辑 `project/src/services/.env`，按当前联调网段配置：

```dotenv
COMPOSE_PROJECT_NAME=yunshao
BACKEND_NETWORK=yunshao_backend
PUBLIC_HOST=192.168.0.1
PUBLIC_ORIGIN=https://192.168.0.1:19080
MQTT_BIND_ADDRESS=192.168.0.1
MQTT_TLS_PORT=8883
MQTT_ADVERTISED_BROKER_URL=ssl://192.168.0.1:8883
POSTGRES_DB=yunshao
POSTGRES_USER=yunshao_bootstrap
VAPID_SUBJECT=mailto:ops@example.com
```

编辑 `project/src/clients/web/.env`。`BACKEND_NETWORK` 必须与后端相同：

```dotenv
BACKEND_NETWORK=yunshao_backend
WEB_BIND_ADDRESS=192.168.0.1
WEB_HTTP_PORT=18080
WEB_HTTPS_PORT=19080
```

如果使用域名，将示例地址替换为浏览器和 Atlas 都能解析/访问的地址。`PUBLIC_ORIGIN` 必须是纯 origin（协议、主机和可选端口），不能带路径。`MQTT_ADVERTISED_BROKER_URL` 必须匹配 broker 证书 SAN 和 Atlas 的实际访问地址。当前 Nginx 的 HTTP 跳转固定指向标准 HTTPS 端口 443；使用 `19080` 时请直接访问 HTTPS 地址，不要依赖 `18080` 跳转。

### 3.1 生成试点证书

在 WSL/Linux 上首次部署时生成证书。mkcert 会创建本机 CA；下面直接把 Web 和 MQTT 的服务器证书及私钥生成到 Compose 使用的目标文件，不需要预先准备密钥，也不经过 `/tmp`：

```sh
umask 077
mkcert -install
mkcert \
  -cert-file project/src/clients/web/.secret-nginx-tls-certificate.pem \
  -key-file project/src/clients/web/.secret-nginx-tls-private-key.pem \
  192.168.0.1
mkcert \
  -cert-file project/src/services/device-service/.secret-mqtt-tls-certificate.pem \
  -key-file project/src/services/device-service/.secret-mqtt-tls-private-key.pem \
  192.168.0.1
cp "$(mkcert -CAROOT)/rootCA.pem" project/src/services/device-service/.secret-mqtt-ca-certificate.pem
chmod 600 project/src/clients/web/.secret-nginx-tls-private-key.pem
chmod 600 project/src/services/device-service/.secret-mqtt-tls-private-key.pem
```

上面从 mkcert CA 目录复制的 `rootCA.pem` 是供客户端验证服务器证书的公开 CA 证书，不是私钥。mkcert 只会把 CA 安装到运行它的系统。让 Windows 浏览器信任此试点 CA 时，将这份 CA 证书复制到 Windows，再在命令提示符中导入当前用户的受信任根证书存储：

```sh
cp "$(mkcert -CAROOT)/rootCA.pem" /mnt/c/Users/Administrator/Downloads/yunshao-rootCA.pem
```

```text
certutil -addstore -user Root "%USERPROFILE%\Downloads\yunshao-rootCA.pem"
```

Atlas 也要信任该 CA，后续会将 `rootCA.pem` 复制到板卡并由 agent 配置引用。不要复制或分发 mkcert 的 `rootCA-key.pem`。生产环境应换成正式证书，并在 Atlas 信任链中安装正式 CA。

## 4. 生成服务机密

在仓库根目录运行以下 POSIX shell 命令。随机数据库密码只使用十六进制字符，可直接写入 PostgreSQL URL：

```sh
set -eu
umask 077
cd project/src/services

openssl rand -hex 32 > auth-service/.secret-postgres-admin-password

for service in auth device event media push; do
  password="$(openssl rand -hex 32)"
  printf '%s\n' "$password" > "$service-service/.secret-database-password"
  printf 'postgres://%s_service:%s@postgres:5432/yunshao?sslmode=disable' \
    "$service" "$password" > "$service-service/.secret-database-url"
done

for file in \
  auth-service/.secret-internal-service-token \
  auth-service/.secret-media-service-token \
  auth-service/.secret-push-service-token \
  device-service/.secret-internal-service-token \
  device-service/.secret-media-service-token \
  event-service/.secret-push-service-token \
  media-service/.secret-nginx-internal-token
do
  openssl rand -hex 32 > "$file"
done

openssl rand -base64 48 | tr -d '\n' > event-service/.secret-cursor-signing-key
openssl rand -base64 32 | tr -d '\n' > media-service/.secret-source-encryption-key
openssl rand -base64 32 | tr -d '\n' > push-service/.secret-encryption-key

openssl rand -hex 32 > device-service/.secret-mqtt-admin-password
openssl rand -hex 32 > device-service/.secret-mqtt-device-service-password
openssl rand -hex 32 > device-service/.secret-mqtt-event-service-password
```

数据库 URL 用户名和数据库名分别必须保持为 `<服务名>_service` 和 `yunshao`；每个 URL 中的密码必须与同目录 `.secret-database-password` 一致。服务间共享 token 的文件按 Compose 映射放在所属服务目录，不要为同一调用关系生成不同的值。

生成 VAPID 密钥对和 Mosquitto 管理选项文件：

```sh
cd project/src/services
npx --yes web-push generate-vapid-keys --json > push-service/.secret-vapid-keys.json
jq -r '.publicKey' push-service/.secret-vapid-keys.json > push-service/.secret-vapid-public-key
jq -r '.privateKey' push-service/.secret-vapid-keys.json > push-service/.secret-vapid-private-key
rm push-service/.secret-vapid-keys.json

mqtt_admin_password="$(cat device-service/.secret-mqtt-admin-password)"
printf '%s\n' \
  '--cafile /run/secrets/mqtt_ca_certificate' \
  '-h mosquitto' \
  '-p 8883' \
  '-u admin' \
  "-P $mqtt_admin_password" \
  > device-service/.secret-mosquitto-ctrl.options
unset mqtt_admin_password
```

建立 MediaMTX API/reader 密碼與配置。`PUBLIC_ORIGIN` 要與服務 `.env` 一致：

```sh
cd project/src/services
openssl rand -hex 32 > media-service/.secret-mediamtx-api-password
openssl rand -hex 32 > media-service/.secret-mediamtx-reader-password
cp media-service/mediamtx.yml.example media-service/mediamtx.yml

api_password="$(cat media-service/.secret-mediamtx-api-password)"
reader_password="$(cat media-service/.secret-mediamtx-reader-password)"
sed -i \
  -e "s|__MEDIAMTX_API_PASSWORD__|$api_password|g" \
  -e "s|__MEDIAMTX_READER_PASSWORD__|$reader_password|g" \
  -e 's|__PUBLIC_ORIGIN__|https://192.168.0.1:19080|g' \
  -e 's|__WEBRTC_ADDITIONAL_HOSTS__|192.168.0.1|g' \
  media-service/mediamtx.yml
unset api_password reader_password
```

生成 Nginx 到 auth/media-service 的內部 header，以及 Nginx 到 MediaMTX 的 Basic Authorization header：

```sh
cd project/src/clients/web
auth_token="$(cat ../../services/auth-service/.secret-internal-service-token)"
media_token="$(cat ../../services/media-service/.secret-nginx-internal-token)"
reader_password="$(cat ../../services/media-service/.secret-mediamtx-reader-password)"
reader_auth="$(printf 'nginx-media-reader:%s' "$reader_password" | base64 | tr -d '\n')"
printf 'proxy_set_header X-Internal-Service-Token "%s";\n' "$auth_token" > .secret-nginx-auth-header.conf
printf 'proxy_set_header X-Internal-Service-Token "%s";\n' "$media_token" > .secret-nginx-media-header.conf
printf 'proxy_set_header Authorization "Basic %s";\n' "$reader_auth" > .secret-nginx-mediamtx-reader-header.conf
chmod 600 .secret-*
unset auth_token media_token reader_password reader_auth
```

以上命令会生成 Compose 所需的服务密码、数据库 URL、内部 token、加密密钥、VAPID 密钥、MQTT TLS 文件和 Nginx header 文件。两个 Compose 文件引用的 `.secret-*` 都必须存在后才能启动。不要把凭据、私钥、激活响应或备份提交到 Git。

## 5. 安装并启动 MediaMTX

从 [MediaMTX 官方 Releases](https://github.com/bluenviron/mediamtx/releases) 选择与部署主机架构匹配的 Linux 单文件发行包。以下以 `v1.21.0`、Linux x86-64 为例；选择其他版本/架构时，下载相应资产和同一 release 的 `checksums.sha256`：

```sh
cd project/src/services/media-service
curl -fLO https://github.com/bluenviron/mediamtx/releases/download/v1.21.0/mediamtx_v1.21.0_linux_amd64.tar.gz
curl -fLO https://github.com/bluenviron/mediamtx/releases/download/v1.21.0/checksums.sha256
grep 'mediamtx_v1.21.0_linux_amd64.tar.gz' checksums.sha256 | sha256sum --check -
tar -xzf mediamtx_v1.21.0_linux_amd64.tar.gz mediamtx
install -m 0755 mediamtx ./mediamtx
./mediamtx --validate-conf=mediamtx.yml
```

本地验收可在独立终端直接运行并保持进程：

```sh
./mediamtx mediamtx.yml
```

Linux 长期部署可使用 systemd 管理该原生进程。确认仓库绝对路径后，将 unit 中 `/srv/yunshao` 替换成实际路径；服务账号 `mediamtx` 必须能读取二进制和配置。`mediamtx.yml` 含密码，应限制读取权限：

```sh
getent passwd mediamtx >/dev/null || sudo useradd --system --no-create-home --shell /usr/sbin/nologin mediamtx
sudo chown root:mediamtx project/src/services/media-service/mediamtx project/src/services/media-service/mediamtx.yml
sudo chmod 0750 project/src/services/media-service/mediamtx
sudo chmod 0640 project/src/services/media-service/mediamtx.yml
```

```ini
[Unit]
Description=Yunshao MediaMTX
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mediamtx
Group=mediamtx
WorkingDirectory=/srv/yunshao/project/src/services/media-service
ExecStart=/srv/yunshao/project/src/services/media-service/mediamtx /srv/yunshao/project/src/services/media-service/mediamtx.yml
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

将 unit 保存为 `/etc/systemd/system/yunshao-mediamtx.service`，并确保该用户可遍历仓库父目录、读取上述两个文件，然后启动：

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now yunshao-mediamtx.service
sudo systemctl status yunshao-mediamtx.service --no-pager
```

## 6. 启动后端与 Web/PWA

### 6.1 后端

先确认 MediaMTX API 在主机 `9997` 端口工作，再校验并启动后端 Compose：

```sh
cd project/src/services
docker compose --env-file .env config --quiet
docker compose --env-file .env up --build -d
docker compose --env-file .env ps
```

首次启动会初始化 PostgreSQL 的五个服务账号/schema、Mosquitto 动态安全角色和服务凭据，再启动五个 Go 服务。等待 `auth-service`、`device-service`、`event-service`、`media-service`、`push-service`、`postgres` 和 `mosquitto` 显示 healthy。检查日志：

```sh
docker compose --env-file .env logs --tail 100
```

### 6.2 Web/PWA

后端创建 `yunshao_backend` 共享网络后，再单独启动 Web/Nginx：

```sh
cd project/src/clients/web
docker compose config --quiet
docker compose up --build -d
docker compose ps
```

从 Windows 浏览器打开 `https://192.168.0.1:19080/`。首次部署没有内置默认账号，在页面注册账号并登录。密码需符合页面的最低长度要求。确认能打开运行总览、实时监控、异常事件、数据分析和边缘设备页面。

确认入口和公开 VAPID key 可访问：

```sh
curl --fail --show-error https://192.168.0.1:19080/
curl --fail --show-error https://192.168.0.1:19080/api/v1/push/vapid-public-key
```

如果当前系统未信任 mkcert CA，`curl` 可加 `--cacert "$(mkcert -CAROOT)/rootCA.pem"`。不要用 `-k` 绕过证书验证。

## 7. 安装 Atlas 端程序并入库模型

板端需要 CANN 与驱动，以及 C++17、CMake、pkg-config、OpenCV 4、OpenSSL、SQLite、Mosquitto、FFmpeg、GStreamer RTSP Server/App 和 `rtph264pay`。常用 Ubuntu/Debian 开发包如下；CANN/驱动须使用与板卡镜像匹配的华为安装包：

```sh
sudo apt-get update
sudo apt-get install -y build-essential cmake pkg-config libopencv-dev libssl-dev libsqlite3-dev libmosquitto-dev libavformat-dev libavcodec-dev libavutil-dev libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev libgstrtspserver-1.0-dev gstreamer1.0-plugins-good nlohmann-json3-dev
```

把整个 Atlas 工程复制到板卡。若板上已有同名源码目录，先改名备份：

```sh
ssh root@192.168.0.2 'if [ -d /root/atlas200idka2 ]; then mv /root/atlas200idka2 /root/atlas200idka2.backup; fi'
scp -r project/src/device_sides/atlas200idka2 root@192.168.0.2:/root/
```

### 7.1 配置 CANN 并构建

SSH 登录 Atlas 后，从板卡已安装的 CANN 环境生成 systemd 与 CMake 共用的 `/etc/yunshao/cann.env`。示例 CANN 脚本路径为 `/usr/local/Ascend/ascend-toolkit/set_env.sh`；若板卡安装路径不同，换成实际路径：

```sh
. /usr/local/Ascend/ascend-toolkit/set_env.sh
ASCEND_TOOLKIT_HOME="${ASCEND_TOOLKIT_HOME:-$ASCEND_HOME_PATH}"
: "${ASCEND_HOME_PATH:?CANN environment did not set ASCEND_HOME_PATH}"
: "${ASCEND_OPP_PATH:?CANN environment did not set ASCEND_OPP_PATH}"
: "${LD_LIBRARY_PATH:?CANN environment did not set LD_LIBRARY_PATH}"
getent group yunshao >/dev/null || sudo groupadd --system yunshao
id yunshao >/dev/null 2>&1 || sudo useradd --system --home-dir /var/lib/yunshao --create-home --shell /usr/sbin/nologin --gid yunshao --groups video,HwHiAiUser,HwBaseUser yunshao
sudo mkdir -p /etc/yunshao
sudo tee /etc/yunshao/cann.env >/dev/null <<EOF
ASCEND_HOME_PATH=$ASCEND_HOME_PATH
ASCEND_TOOLKIT_HOME=$ASCEND_TOOLKIT_HOME
ASCEND_OPP_PATH=$ASCEND_OPP_PATH
LD_LIBRARY_PATH=$LD_LIBRARY_PATH
EOF
sudo chown root:yunshao /etc/yunshao/cann.env
sudo chmod 0640 /etc/yunshao/cann.env
```

如果 `yunshao` 用户已经存在，检查并补齐 `video`、`HwHiAiUser`、`HwBaseUser` 附加组。再构建、运行板端测试并安装：

```sh
cd /root/atlas200idka2
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DYUNSHAO_CANN_ENV_FILE=/etc/yunshao/cann.env
cmake --build build -j2
ctest --test-dir build --output-on-failure
sudo cmake --install build --prefix /usr/local
sudo udevadm control --reload-rules
sudo udevadm trigger
sudo systemctl daemon-reload
```

安装会把程序、课堂模型插件、默认 JSON 配置、systemd unit 和 `/dev/rgn` udev 规则放入系统目录。首次安装直接使用默认配置；升级时先备份 `/etc/yunshao/` 中的本地 JSON/env 配置，安装后再合并。

### 7.2 创建设备并签发激活票据

在 Web“边缘设备”页面创建 Atlas 设备，IP 填 `192.168.0.2`。打开设备详情并签发一次性激活票据；票据有效期 10 分钟，只显示一次。不要将票据发给其他人或写入仓库。

把 MQTT CA 复制到 Atlas：

```sh
scp project/src/services/device-service/.secret-mqtt-ca-certificate.pem root@192.168.0.2:/tmp/yunshao-mqtt-ca.pem
```

SSH 到 Atlas，安全输入设备 UUID 和一次性票据并请求激活。激活响应会返回一次性 MQTT `client_id`、用户名和密码。下面的示例不会回显票据，并把响应限制为 root 可读：

```sh
read -r -p 'Device UUID: ' DEVICE_ID
read -r -s -p 'One-time activation ticket: ' ACTIVATION_TICKET
printf '\n'
umask 077
jq -n --arg device_id "$DEVICE_ID" --arg ticket "$ACTIVATION_TICKET" '{device_id:$device_id,ticket:$ticket}' \
  | curl --fail --silent --show-error --cacert /tmp/yunshao-mqtt-ca.pem \
      -H 'Content-Type: application/json' --data-binary @- \
      https://192.168.0.1:19080/edge/v1/activate \
      --output /root/yunshao-activation.json
unset ACTIVATION_TICKET
```

从响应生成 `/etc/yunshao/agent.env`。`YUNSHAO_MQTT_HOST` 与端口取自响应 `mqtt.broker_url` 的 `ssl://host:port`：

```sh
BROKER="$(jq -r '.mqtt.broker_url | sub("^ssl://"; "")' /root/yunshao-activation.json)"
BROKER_HOST="${BROKER%:*}"
BROKER_PORT="${BROKER##*:}"
ACTIVATED_DEVICE_ID="$(jq -r '.device_id' /root/yunshao-activation.json)"
CLIENT_ID="$(jq -r '.mqtt.client_id' /root/yunshao-activation.json)"
MQTT_USERNAME="$(jq -r '.mqtt.username' /root/yunshao-activation.json)"
MQTT_PASSWORD="$(jq -r '.mqtt.password' /root/yunshao-activation.json)"
sudo install -o root -g yunshao -m 0640 /tmp/yunshao-mqtt-ca.pem /etc/yunshao/mqtt-ca.pem
printf 'YUNSHAO_DEVICE_ID=%s\nYUNSHAO_MQTT_HOST=%s\nYUNSHAO_MQTT_PORT=%s\nYUNSHAO_MQTT_CLIENT_ID=%s\nYUNSHAO_MQTT_USERNAME=%s\nYUNSHAO_MQTT_PASSWORD=%s\nYUNSHAO_MQTT_CA=/etc/yunshao/mqtt-ca.pem\n' \
  "$ACTIVATED_DEVICE_ID" "$BROKER_HOST" "$BROKER_PORT" "$CLIENT_ID" "$MQTT_USERNAME" "$MQTT_PASSWORD" \
  | sudo tee /etc/yunshao/agent.env >/dev/null
sudo chown root:yunshao /etc/yunshao/agent.env
sudo chmod 0640 /etc/yunshao/agent.env
rm -f /root/yunshao-activation.json /tmp/yunshao-mqtt-ca.pem
unset BROKER BROKER_HOST BROKER_PORT ACTIVATED_DEVICE_ID CLIENT_ID MQTT_USERNAME MQTT_PASSWORD DEVICE_ID
```

启动 agent 并确认设备在 Web 中上线：

```sh
sudo systemctl enable --now yunshao-agent.service
sudo systemctl status yunshao-agent.service --no-pager
sudo journalctl -u yunshao-agent.service -n 50 --no-pager
```

### 7.3 安装课堂 OM 模型

仓库提供模型 OM、manifest 和课堂模型 profile。manifest 中的 `model_file` 是包内文件名；安装器校验 SHA-256、SoC、输入形状、类别顺序并在 NPU 上试加载：

```sh
scp project/preparation/model_classroom_behavior/model_deployment/model_classroom_behavior_detection.manifest.json root@192.168.0.2:/tmp/
scp project/preparation/model_classroom_behavior/model_deployment/yolo11n_classroom_behavior_best.om root@192.168.0.2:/tmp/model.om
ssh root@192.168.0.2 'sudo -u yunshao /usr/local/bin/yunshao-agent model install --manifest /tmp/model_classroom_behavior_detection.manifest.json --model /tmp/model.om'
ssh root@192.168.0.2 'sudo -u yunshao /usr/local/bin/yunshao-agent model status model_classroom_behavior_detection'
```

状态应为 `installed`，类别顺序为 `hand-raising`、`reading`、`writing`。模型转换或文件变更后，按[课堂模型转换 SOP](project/preparation/model_classroom_behavior/model_convert/README.md) 重新转换并生成匹配 manifest；未在目标 Atlas 上加载的 OM 不应启用检测流。

## 8. 配置并启动课堂视频流

首次安装默认关闭视频流和告警。告警规则继续保持 `enabled: false`。要处理画面，只对 `/etc/yunshao/streams_config.json` 的 `classroom-demo` 设置：

- `enabled: true`
- `enable_ai: true`
- `rtsp_output.enabled: true`
- `rtsp_output.listen_address: "0.0.0.0"`
- `rtsp_output.port: 8554`
- `rtsp_output.mount_path: "/classroom-demo"`

创建本次测试的输入环境文件并启动 runtime：

```sh
sudo tee /etc/yunshao/classroom-runtime.env >/dev/null <<'EOF'
YUNSHAO_CAMERA_CLASSROOM_DEMO_URL=udp://0.0.0.0:5000
EOF
sudo chown root:yunshao /etc/yunshao/classroom-runtime.env
sudo chmod 0640 /etc/yunshao/classroom-runtime.env
sudo systemctl enable --now yunshao-classroom-runtime.service
sudo systemctl status yunshao-classroom-runtime.service --no-pager
sudo ss -lnptu | grep -E '(:5000|:8554)'
```

默认输出地址为 `rtsp://192.168.0.2:8554/classroom-demo`。云端必须能路由到它。实际接摄像头时，将 `YUNSHAO_CAMERA_CLASSROOM_DEMO_URL` 改为摄像头可访问的 H.264 流地址；端侧 RTSP 输出不需要账号密码。

## 9. 配置浏览器摄像头并端到端验证

Web 中进入设备详情，添加或选择摄像头，在视频来源处填写 Atlas 输出地址：

```text
rtsp://192.168.0.2:8554/classroom-demo
```

保存后进入“实时监控”。MediaMTX 会按需从 Atlas 拉流；浏览器只通过受保护的 WebRTC/HLS 地址观看，不会得到 RTSP 来源地址。WebRTC 使用内网 UDP `8189`；该网络不可达时页面会尝试 HLS。

没有摄像头时，用仓库视频测试。先在 Windows 浏览器打开“实时监控”并请求对应摄像头画面，再在 Windows 命令提示符的仓库根目录持续推流：

```text
ffmpeg -re -stream_loop -1 -i video/classroom_behavior.mp4 -map 0:v:0 -vf "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2:black,fps=15" -an -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p -g 30 -bf 0 -b:v 700k -f mpegts "udp://192.168.0.2:5000?pkt_size=1316"
```

“实时监控”应显示视频和模型检测框。框的类别取决于视频内容与模型预测；告警列表初始为空，因为告警规则默认关闭。完成后在 Windows 推流终端按 `Ctrl+C` 停流。板端推流/播放详细 SOP 见 [Atlas 部署与端到端测试](project/src/device_sides/atlas200idka2/README.md)。

## 10. 日常检查与安全停止

后端状态与日志：

```sh
cd project/src/services
docker compose --env-file .env ps
docker compose --env-file .env logs --tail 100 auth-service device-service event-service media-service push-service
```

Web 状态与日志：

```sh
cd project/src/clients/web
docker compose ps
docker compose logs --tail 100 web
```

Atlas 状态与日志：

```sh
sudo systemctl is-active yunshao-agent.service yunshao-classroom-runtime.service
sudo journalctl -u yunshao-agent.service -n 100 --no-pager
sudo journalctl -u yunshao-classroom-runtime.service -n 100 --no-pager
sudo -u yunshao /usr/local/bin/yunshao-agent model status model_classroom_behavior_detection
```

停止服务但保留数据库、Mosquitto 状态和事件证据：

```sh
cd project/src/clients/web && docker compose stop
cd ../../services && docker compose --env-file .env stop
sudo systemctl stop yunshao-classroom-runtime.service yunshao-agent.service yunshao-mediamtx.service
```

日常停止或升级不要执行 `docker compose down -v`，该命令会删除持久化卷。数据库备份与恢复见[后端运维说明](project/src/services/README.md#postgresql-备份与恢复)。更新后端/Web 时，分别在对应 Compose 目录执行 `docker compose up --build -d`。Atlas 工程更新时重新复制整个目录、构建、运行 CTest 并安装，详见 [Atlas README](project/src/device_sides/atlas200idka2/README.md)。

## 11. 常见问题

| 现象 | 检查方向 |
| --- | --- |
| 后端某服务不是 healthy | 查看服务日志、`.env` 和其 owner 目录的 `.secret-*` 文件；确认数据库和 broker bootstrap 成功。 |
| media-service 未就绪 | 检查 MediaMTX 进程、主机 `9997` API 和 `.secret-mediamtx-api-password` 是否一致。 |
| Web 显示证书错误 | 确认 URL 主机/IP 在证书 SAN 中；试点环境把 mkcert `rootCA.pem` 安装到浏览器操作系统的信任根。 |
| Atlas MQTT 离线 | 核对激活响应中的 broker 地址、`client_id`、用户名和密码；确认证书 CA 路径、防火墙和系统时间。不要从 device ID 推导 client ID。 |
| 模型未安装或加载失败 | 核对 manifest SHA-256、profile 输入/类别顺序、OM SoC 版本和 CANN 环境；查看 `model status` 错误码。 |
| Atlas 在线但页面无画面 | 检查 runtime、流启用开关、输入 URL、Atlas `8554/TCP`、云端路由、MediaMTX 路径和浏览器 `8189/UDP`。设备在线与视频播放状态相互独立。 |
| FFmpeg 推流后板端无画面 | 确认 Windows → `192.168.0.2:5000/UDP` 可达、推流仍在运行，并先在 Web 实时监控打开摄像头；测试视频需按上面的命令转为 H.264。 |
