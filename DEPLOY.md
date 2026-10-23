# 云哨部署指南

本文包含从零部署步骤，以及当前 `172.20.10.4` 电脑与 `172.20.10.2` Atlas 的局域网联调说明。部署命令集中维护于本文；仓库不使用 PowerShell 脚本。

这份指南按实际部署顺序，从空白部署机开始启动云端、Web/PWA、MediaMTX 和 Atlas 边缘端，最后用仓库中的测试视频完成浏览器端到端验证。

目前聯調主機有兩條網路：Atlas 直連地址 `192.168.0.1`（Atlas 為 `192.168.0.2`），以及團隊 Wi‑Fi 地址 `172.50.53.131`。瀏覽器使用 `https://172.50.53.131:19080`；Windows + Docker Desktop 的 HTTP `18081` 會跳轉至 HTTPS，Linux 主機的 HTTP 入口為 `18080`。部署到其他網路時，應替換為穩定且客戶端可達的 Wi‑Fi 地址或內網 DNS 名稱，並為主機保留 DHCP 地址。目前 Wi‑Fi 網段是 `172.50.53.0/24`，不屬於 RFC 1918 私網地址段；確認網路入口 ACL/防火牆只允許團隊網段存取，且沒有網際網路入站轉發後再開放服務。

## 先准备好这些

- 一台 Linux 部署机，或启用了 Docker Desktop WSL Integration 的 WSL。准备 Docker Compose v2。
- 部署机安装 git、curl、openssl、mkcert、jq、base64、sha256sum、tar。生成 Web Push 密钥需要 Node.js 和 npm。
- 一台已安装匹配版本 Ascend 驱动及 CANN 的 Atlas 200I DK A2。
- Windows 安装浏览器和 FFmpeg，终端中能运行 ffmpeg、ffplay、ssh 和 scp。
- Windows、部署机、Atlas 网络互通。

## 华为云 KooCLI（hcloud）

需要通过华为云 API 查询账号域或申请临时 STS 凭据时，安装 KooCLI。仓库内发布工具要求 KooCLI 3.2.0 或更新版本，建议使用华为云[官方安装指南](https://support.huaweicloud.com/qs-hcli/hcli_02_003_01.html)提供的当前版本。Windows 可从华为云官方对象存储下载 [Windows x64 安装包](https://cn-north-4-hdn-koocli.obs.cn-north-4.myhuaweicloud.com/cli/latest/huaweicloud-cli-windows-amd64.zip)，用文件资源管理器解压，并把包含 `hcloud.exe` 的目录加入用户 PATH。建议安装到 `%LOCALAPPDATA%\Programs\.huawei\bin`，不要放在临时目录。更新 PATH 后打开新终端验证：

```text
hcloud version
```

首次配置时按提示输入 AK/SK 和默认区域；KooCLI 会在本地加密保存认证信息。只更新默认区域时，使用 `--cli-region` 参数：

```text
hcloud configure init
hcloud configure set --cli-region=cn-north-4
```

也可通过环境变量提供凭据：`HUAWEICLOUD_SDK_AK`、`HUAWEICLOUD_SDK_SK`，临时凭据另设 `HUAWEICLOUD_SDK_SECURITY_TOKEN`。不要把凭据写入仓库、命令示例或提交记录。验证账号域查询：

```text
hcloud IAM KeystoneListAuthDomains
```

Linux/macOS 按华为云[官方 Linux 安装指南](https://support.huaweicloud.com/qs-hcli/hcli_02_003_02.html)下载对应系统和 CPU 架构的发行包，解压后把 `hcloud` 放入 `~/.local/bin` 或其他用户级 PATH 目录，再使用相同的验证、配置和查询命令。

防火墙按需开放以下端口：

| 端口 | 方向/来源范围 | 用途 |
| --- | --- | --- |
| 18080/TCP（Linux）／18081/TCP（Windows Docker Desktop） | 團隊 Wi‑Fi 客戶端到部署機 | Web HTTP，跳轉到 HTTPS |
| 19080/TCP | 團隊 Wi‑Fi 客戶端；Atlas `192.168.0.2` 到部署機 | Web HTTPS 與設備激活 |
| 8883/TCP | 仅 Atlas `192.168.0.2` 到部署机 | MQTT over TLS |
| 8554/TCP | 部署机到 Atlas | Atlas 输出的 RTSP 视频 |
| 8189/UDP | 团队 Wi‑Fi 浏览器到部署机 | WebRTC 视频 |
| 5000/UDP | Windows 到 Atlas | 本指南的测试视频输入 |

PostgreSQL、Go 服務端口和 MediaMTX 管理 API 不要開放給局域網或網際網路。將 Linux 的 `18080/TCP` 或 Windows 的 `18081/TCP`，以及 `19080/TCP` 入站規則限制為團隊 Wi‑Fi 網段 `172.50.53.0/24`，另允許 Atlas `192.168.0.2` 存取 `19080/TCP`；MQTT `8883/TCP` 只允許 Atlas。生產環境請使用正式憑證；mkcert 僅用於開發和內網試點。若團隊 Wi‑Fi 啟用了客戶端隔離，其他無線終端仍無法互訪，需由網路管理員開放同網段存取。

## 1. 获取项目

在 Linux 或 WSL 终端中运行：

```sh
git clone --branch dev https://atomgit.com/milo-ally-stu/Yunshao.git
cd Yunshao
```

后续命令从仓库根目录执行；命令中有 cd 时按提示进入对应目录。

## 2. 设置地址并生成证书

复制环境模板：

```sh
cp project/src/services/.env.example project/src/services/.env
cp project/src/clients/web/.env.example project/src/clients/web/.env
```

用 vim 编辑后端配置：

```sh
vim project/src/services/.env
```

设置后端地址。`PUBLIC_ORIGIN` 是浏览器/PWA 的来源；`PUBLIC_HOST` 保留为 Atlas 直连地址：

```dotenv
COMPOSE_PROJECT_NAME=yunshao
BACKEND_NETWORK=yunshao_backend
PUBLIC_HOST=192.168.0.1
PUBLIC_ORIGIN=https://172.50.53.131:19080
# HTTPS origin reachable by Atlas for web-based first-time onboarding and activation.
ATLAS_ACTIVATION_ORIGIN=https://192.168.0.1:19080
MQTT_BIND_ADDRESS=192.168.0.1
MQTT_TLS_PORT=8883
MQTT_ADVERTISED_BROKER_URL=ssl://192.168.0.1:8883
POSTGRES_DB=yunshao
POSTGRES_USER=yunshao_bootstrap
VAPID_SUBJECT=mailto:ops@example.com
```

再编辑 Web 配置：

```sh
vim project/src/clients/web/.env
```

配置浏览器网卡为 Wi‑Fi 地址，并配置第二个绑定地址保留 Atlas 直连激活：

```dotenv
BACKEND_NETWORK=yunshao_backend
WEB_BIND_ADDRESS=172.50.53.131
WEB_ATLAS_BIND_ADDRESS=192.168.0.1
WEB_HTTP_PORT=18080
WEB_HTTPS_PORT=19080
WEB_HTTPS_REDIRECT_PORT=19080
```

若部署在 Linux 主機，使用上面的雙介面設定和 `docker-compose.atlas.yaml`。若在 Windows + Docker Desktop/WSL 上執行，Compose 連接埠繼續繫結 loopback：`WEB_BIND_ADDRESS=127.0.0.1`、`WEB_HTTP_PORT=18080`、`WEB_HTTPS_PORT=18443`、`WEB_HTTPS_REDIRECT_PORT=19080`。Web 容器啟動後，以系統管理員身分開啟命令提示字元，為 Wi‑Fi 地址增加轉發，並將防火牆來源限制在目前 Wi‑Fi 子網：

```text
netsh interface portproxy add v4tov4 listenaddress=172.50.53.131 listenport=18081 connectaddress=127.0.0.1 connectport=18080 protocol=tcp
netsh interface portproxy add v4tov4 listenaddress=172.50.53.131 listenport=19080 connectaddress=127.0.0.1 connectport=18443 protocol=tcp
netsh advfirewall firewall add rule name="Yunshao Web WiFi 172.50.53.0-24" dir=in action=allow protocol=TCP localip=172.50.53.131 localport=18081,19080 remoteip=172.50.53.0/24 profile=public
```

Windows Docker Desktop 已將 HTTP 對外端口設為 `18081`，因為容器的本機 HTTP 發布端口 `18080` 已被占用。上述 Windows 轉發與防火牆規則只作用於 Wi‑Fi 地址和目前 `/24` 來源；既有 `192.168.0.1` Atlas portproxy 保持不變。若客戶端啟用了系統代理，請將 `172.50.53.131` 加入代理繞過清單，讓瀏覽器直接連到 Wi‑Fi 主機。重複部署前先用 `netsh interface portproxy show all` 和 `netsh advfirewall firewall show rule name="Yunshao Web WiFi 172.50.53.0-24"` 檢查現有規則。網卡地址變更時，更新轉發、Origin、憑證 SAN 和 MediaMTX 設定。

为 Web 和 MQTT 生成开发证书。mkcert 会在本机创建 CA，并把服务器证书和私钥直接写入服务使用的文件：

```sh
umask 077
mkcert -install
mkcert -cert-file project/src/clients/web/.secret-nginx-tls-certificate.pem -key-file project/src/clients/web/.secret-nginx-tls-private-key.pem 172.50.53.131 192.168.0.1
mkcert -cert-file project/src/services/device-service/.secret-mqtt-tls-certificate.pem -key-file project/src/services/device-service/.secret-mqtt-tls-private-key.pem 192.168.0.1
cp "$(mkcert -CAROOT)/rootCA.pem" project/src/services/device-service/.secret-mqtt-ca-certificate.pem
cp "$(mkcert -CAROOT)/rootCA.pem" project/src/services/device-service/.secret-atlas-activation-ca-certificate.pem
chmod 600 project/src/clients/web/.secret-nginx-tls-private-key.pem
chmod 600 project/src/services/device-service/.secret-mqtt-tls-private-key.pem
```

让每台使用 Web/PWA 的客户端设备信任签发 Web 证书的 CA。按本节从零部署时，以下命令将 mkcert 的公开 CA 证书复制到当前 Windows 用户的下载目录；若沿用现有本机开发 CA，则使用 `project/src/services/device-service/.secret-mqtt-ca-certificate.pem`：

```sh
cp "$(mkcert -CAROOT)/rootCA.pem" /mnt/c/Users/Administrator/Downloads/yunshao-rootCA.pem
```

在 Windows 命令提示符中导入证书：

```text
certutil -addstore -user Root "%USERPROFILE%\Downloads\yunshao-rootCA.pem"
```

在每位同事的 Windows 设备上，通过可信的内部渠道提供 `rootCA.pem` 并执行导入命令；macOS、Linux 和手机也需将该公开证书加入系统信任库。不要复制或分享 `rootCA-key.pem`。生产环境应改用组织信任的正式证书。Atlas 首次网页接入所需 CA 已由后端 Compose 提供，设备端自动安装；第 6 节说明的是已有设备如何单独信任 CA。

## 3. 生成服务密钥

所有密钥都在首次部署时生成，不需要从临时目录或其他机器复制。以下命令在 Linux/WSL 仓库根目录执行。

先生成 PostgreSQL 管理密码、每个服务的数据库密码和对应数据库 URL：

```sh
cd project/src/services
umask 077

openssl rand -hex 32 > auth-service/.secret-postgres-admin-password

openssl rand -hex 32 > auth-service/.secret-database-password
printf 'postgres://auth_service:%s@postgres:5432/yunshao?sslmode=disable' "$(cat auth-service/.secret-database-password)" > auth-service/.secret-database-url

openssl rand -hex 32 > device-service/.secret-database-password
printf 'postgres://device_service:%s@postgres:5432/yunshao?sslmode=disable' "$(cat device-service/.secret-database-password)" > device-service/.secret-database-url

openssl rand -hex 32 > event-service/.secret-database-password
printf 'postgres://event_service:%s@postgres:5432/yunshao?sslmode=disable' "$(cat event-service/.secret-database-password)" > event-service/.secret-database-url

openssl rand -hex 32 > media-service/.secret-database-password
printf 'postgres://media_service:%s@postgres:5432/yunshao?sslmode=disable' "$(cat media-service/.secret-database-password)" > media-service/.secret-database-url

openssl rand -hex 32 > push-service/.secret-database-password
printf 'postgres://push_service:%s@postgres:5432/yunshao?sslmode=disable' "$(cat push-service/.secret-database-password)" > push-service/.secret-database-url
```

再生成服务间调用凭据、加密密钥和 MQTT 密码：

```sh
openssl rand -hex 32 > auth-service/.secret-internal-service-token
openssl rand -hex 32 > auth-service/.secret-media-service-token
openssl rand -hex 32 > auth-service/.secret-push-service-token
openssl rand -hex 32 > device-service/.secret-internal-service-token
openssl rand -hex 32 > device-service/.secret-media-service-token
openssl rand -hex 32 > event-service/.secret-push-service-token
openssl rand -hex 32 > media-service/.secret-nginx-internal-token

openssl rand -base64 48 | tr -d '\n' > event-service/.secret-cursor-signing-key
openssl rand -base64 32 | tr -d '\n' > media-service/.secret-source-encryption-key
openssl rand -base64 32 | tr -d '\n' > push-service/.secret-encryption-key

openssl rand -hex 32 > device-service/.secret-mqtt-admin-password
openssl rand -hex 32 > device-service/.secret-mqtt-device-service-password
openssl rand -hex 32 > device-service/.secret-mqtt-event-service-password
openssl rand -hex 32 > media-service/.secret-mediamtx-api-password
openssl rand -hex 32 > media-service/.secret-mediamtx-reader-password
```

生成 Web Push 密钥：

```sh
npx --yes web-push generate-vapid-keys --json > push-service/.secret-vapid-keys.json
jq -r '.publicKey' push-service/.secret-vapid-keys.json > push-service/.secret-vapid-public-key
jq -r '.privateKey' push-service/.secret-vapid-keys.json > push-service/.secret-vapid-private-key
rm push-service/.secret-vapid-keys.json
```

生成 Mosquitto 管理文件：

```sh
printf '%s\n' \
  '--cafile /run/secrets/mqtt_ca_certificate' \
  '-h mosquitto' \
  '-p 8883' \
  '-u admin' \
  "-P $(cat device-service/.secret-mqtt-admin-password)" \
  > device-service/.secret-mosquitto-ctrl.options
```

生成 Nginx 调用后端和 MediaMTX 的请求头文件：

```sh
cd ../clients/web
printf 'proxy_set_header X-Internal-Service-Token "%s";\n' "$(cat ../../services/auth-service/.secret-internal-service-token)" > .secret-nginx-auth-header.conf
printf 'proxy_set_header X-Internal-Service-Token "%s";\n' "$(cat ../../services/media-service/.secret-nginx-internal-token)" > .secret-nginx-media-header.conf
printf 'proxy_set_header Authorization "Basic %s";\n' "$(printf 'nginx-media-reader:%s' "$(cat ../../services/media-service/.secret-mediamtx-reader-password)" | base64 | tr -d '\n')" > .secret-nginx-mediamtx-reader-header.conf
```

服务密钥已生成。接着安装 MediaMTX，并按下一节创建本地配置。

## 4. 安装 MediaMTX

在 Linux 部署机上下载官方单文件程序并校验文件：

```sh
cd ../../services/media-service
curl -fLO https://github.com/bluenviron/mediamtx/releases/download/v1.21.0/mediamtx_v1.21.0_linux_amd64.tar.gz
curl -fLO https://github.com/bluenviron/mediamtx/releases/download/v1.21.0/checksums.sha256
grep 'mediamtx_v1.21.0_linux_amd64.tar.gz' checksums.sha256 | sha256sum --check -
tar -xzf mediamtx_v1.21.0_linux_amd64.tar.gz mediamtx
install -m 0755 mediamtx ./mediamtx
```

如果部署机不是 Linux x86-64，下载对应系统和架构的官方发行包。

复制 MediaMTX 配置模板：

```sh
cp mediamtx.yml.example mediamtx.yml
```

查看密码并用 vim 修改配置：

```sh
cat .secret-mediamtx-api-password
cat .secret-mediamtx-reader-password
vim mediamtx.yml
```

在配置中替换以下四个示例值：

- __MEDIAMTX_API_PASSWORD__：刚才输出的 API 密码。
- __MEDIAMTX_READER_PASSWORD__：刚才输出的播放密码。
- __PUBLIC_ORIGIN__：https://172.50.53.131:19080
- __WEBRTC_ADDITIONAL_HOSTS__：172.50.53.131, 192.168.0.1

保存后限制配置文件读取权限，并校验配置：

```sh
chmod 600 mediamtx.yml
./mediamtx --validate-conf=mediamtx.yml
```

MediaMTX 由部署机直接运行，不放进 Docker Compose。试运行时，在一个终端中运行下面命令并保持终端打开：

```sh
./mediamtx mediamtx.yml
```

正式部署时用 systemd 管理 MediaMTX。先按 Ctrl+C 停掉前面的试运行进程，再用 vim 新建 /etc/systemd/system/yunshao-mediamtx.service。将路径替换成仓库的实际绝对路径：

```sh
sudo vim /etc/systemd/system/yunshao-mediamtx.service
```

```ini
[Unit]
Description=Yunshao MediaMTX
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/仓库绝对路径/project/src/services/media-service
ExecStart=/仓库绝对路径/project/src/services/media-service/mediamtx /仓库绝对路径/project/src/services/media-service/mediamtx.yml
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

保存后启动：

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now yunshao-mediamtx.service
sudo systemctl status yunshao-mediamtx.service --no-pager
```



## 5. 启动云端和 Web

在部署机新开一个终端，并从仓库根目录开始执行以下命令。Compose 会初始化 PostgreSQL 和 Mosquitto，再启动各 Go 微服务：

```sh
cd project/src/services
docker compose --env-file .env config --quiet
docker compose --env-file .env up --build -d
docker compose --env-file .env ps
```

确认服务状态为 healthy。然后单独启动 Web/PWA：

```sh
cd ../clients/web
docker compose -f docker-compose.yaml -f docker-compose.atlas.yaml config --quiet
docker compose -f docker-compose.yaml -f docker-compose.atlas.yaml up --build -d
docker compose -f docker-compose.yaml -f docker-compose.atlas.yaml ps
```

团队 Wi-Fi 客户端浏览器打开 `https://172.50.53.131:19080/`。Atlas agent 使用其预置的 `YUNSHAO_EDGE_API_ORIGIN` 经 HTTPS 后台轮询设备激活请求；用户登录后在设备详情选择期限并点击网页激活/续期，不需要在 Atlas 终端运行激活命令。首次部署没有默认账号，请先在页面注册并登录。

## 6. 安装 Atlas 程序、激活设备并安装模型

在 Atlas 上安装构建依赖。Ascend 驱动和 CANN 使用与板卡系统匹配的华为安装包：

```sh
sudo apt-get update
sudo apt-get install -y \
  build-essential cmake pkg-config \
  libopencv-dev libssl-dev libsqlite3-dev libcurl4-openssl-dev \
  libmosquitto-dev libavformat-dev libavcodec-dev libavutil-dev \
  libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
  libgstrtspserver-1.0-dev gstreamer1.0-plugins-good \
  nlohmann-json3-dev
```

从 Windows 的仓库根目录把整个板端工程复制到 Atlas。板上已有同名源码目录时，先备份移走旧目录，避免残留旧文件：

```text
ssh root@192.168.0.2 'if [ -d /root/atlas200idka2 ]; then mv /root/atlas200idka2 /root/atlas200idka2.backup; fi'
```

再执行复制：

```text
scp -r project/src/device_sides/atlas200idka2 root@192.168.0.2:/root/
```

SSH 登录板卡后加载 CANN 环境，并创建 yunshao 系统用户。已有用户时也可以执行：

```sh
. /usr/local/Ascend/ascend-toolkit/set_env.sh
export ASCEND_TOOLKIT_HOME=${ASCEND_TOOLKIT_HOME:-$ASCEND_HOME_PATH}
getent group yunshao >/dev/null || sudo groupadd --system yunshao
id yunshao >/dev/null 2>&1 || sudo useradd --system --home-dir /var/lib/yunshao --create-home --shell /usr/sbin/nologin --gid yunshao yunshao
sudo usermod -aG video,HwHiAiUser,HwBaseUser yunshao
sudo mkdir -p /etc/yunshao
env | grep -E "^(ASCEND_HOME_PATH|ASCEND_TOOLKIT_HOME|ASCEND_OPP_PATH|LD_LIBRARY_PATH)="
sudo vim /etc/yunshao/cann.env
```

把刚才显示的四项原样写入 cann.env，每项一行，格式为 NAME=value。保存后设置文件权限：

```sh
sudo chown root:yunshao /etc/yunshao/cann.env
sudo chmod 0640 /etc/yunshao/cann.env
```

构建、运行板端测试并安装程序：

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

首次部署先在 Web 登记设备并记录生成的 device ID；管理员再按受控设备安装流程，将该 ID、初始可信 MQTT 身份、CA 证书和云端 HTTPS Origin 绑定到 Atlas agent，并启用 `yunshao-agent.service`。初始身份必须同时由服务端登记，agent 才能用它建立 TLS 轮询；agent 需能访问 `https://192.168.0.1:19080`。这是一次性的设备部署/接入工作，终端用户日常激活与续期无需登录 Atlas 或执行命令。切勿把凭据写入浏览器或普通部署文档。

完成 agent 预置并确认设备在线后，用户在 Web 打开对应设备记录（不要重复登记）。选择“无限期”或输入自定义期限（分钟、小时、天），点击“网页激活设备”或“网页续期并激活”。页面等待 Atlas 确认保存新凭据后才显示完成；agent 在后台通过 TLS 领取轮换后的 MQTT 凭据，写入仅设备服务用户可读的 `/var/lib/yunshao/activation.env`，自动重启自身并同步本地租期。浏览器不显示票据、激活命令或 MQTT 密码。

有限期授权从服务端确认激活时开始。到期后 device-service 自动将设备置为 `expired` 并撤销 MQTT 权限；Atlas classroom runtime 根据本地租期暂停，agent 保留 HTTPS 轮询以便用户网页续期。续期成功后新租期和凭据由 agent 自动接收，runtime 恢复。检查服务可使用部署阶段的运维方式；终端用户日常续期不需登录 Atlas。
安装仓库中的课堂 OM 模型：

```text
scp project/preparation/model_classroom_behavior/model_deployment/model_classroom_behavior_detection.manifest.json root@192.168.0.2:/tmp/
scp project/preparation/model_classroom_behavior/model_deployment/yolo11n_classroom_behavior_best.om root@192.168.0.2:/tmp/model.om
ssh root@192.168.0.2 'sudo -u yunshao /usr/local/bin/yunshao-agent model install --manifest /tmp/model_classroom_behavior_detection.manifest.json --model /tmp/model.om'
ssh root@192.168.0.2 'sudo -u yunshao /usr/local/bin/yunshao-agent model status model_classroom_behavior_detection'
```

状态应为 installed。模型未成功入库时不要启用 AI 视频流。

## 7. 启动测试视频流

先在 Atlas 编辑课堂流配置：

```sh
sudo vim /etc/yunshao/streams_config.json
```

在 classroom-demo 这项中把 enabled、enable_ai 和 rtsp_output.enabled 改为 true。告警规则保持关闭，不需要编辑 alarm_rules.json。

设置测试视频输入地址：

```sh
sudo vim /etc/yunshao/classroom-runtime.env
```

文件内容：

```text
YUNSHAO_CAMERA_CLASSROOM_DEMO_URL=udp://0.0.0.0:5000
```

保存并启动课堂 runtime：

```sh
sudo chown root:yunshao /etc/yunshao/classroom-runtime.env
sudo chmod 0640 /etc/yunshao/classroom-runtime.env
sudo systemctl enable --now yunshao-classroom-runtime.service
sudo systemctl status yunshao-classroom-runtime.service --no-pager
```

Atlas 输出地址为：

```text
rtsp://192.168.0.2:8554/classroom-demo
```

## 8. 浏览器端到端验收

先在 Web 的边缘设备页面添加摄像头，将视频来源填写为上面的 RTSP 地址。保存后打开实时监控页面。

在 Windows 命令提示符中进入仓库根目录，循环推送测试视频：

```text
ffmpeg -re -stream_loop -1 -i video/classroom_behavior.mp4 ^
  -map 0:v:0 ^
  -vf "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2:black,fps=15" ^
  -an -c:v libx264 -preset ultrafast -tune zerolatency ^
  -pix_fmt yuv420p -g 30 -bf 0 -b:v 700k ^
  -f mpegts "udp://192.168.0.2:5000?pkt_size=1316"
```

回到浏览器的实时监控页面，应看到视频画面和模型检测结果。告警列表预期为空，因为当前规则全部关闭。也可以在 Windows 直接检查 Atlas RTSP 输出：

```text
ffplay -rtsp_transport tcp "rtsp://192.168.0.2:8554/classroom-demo"
```

测试结束时，在 FFmpeg 终端按 Ctrl+C。停止服务但保留数据库和运行数据：

```sh
cd project/src/clients/web
docker compose stop
cd ../../services
docker compose --env-file .env stop
```

Atlas 可按需停止：

```sh
sudo systemctl stop yunshao-classroom-runtime.service yunshao-agent.service yunshao-mediamtx.service
```

## 出现问题时先看这里

| 现象 | 先检查 |
| --- | --- |
| Compose 校验失败 | 两个 .env 是否填写；Compose 提示缺少哪个 .secret 文件，就检查对应 service 目录。 |
| Web 提示证书不受信任 | Windows 是否导入了 mkcert 的 rootCA.pem；访问地址是否与证书中的 IP/域名一致。 |
| Atlas 显示离线 | agent.env 中的 MQTT 地址和激活响应是否一致；设备时间是否正确；CA 路径和 8883/TCP 是否可达。 |
| Atlas 在线但没有视频 | classroom-demo 的三个开关是否开启；测试 runtime 是否设置 UDP/5000 输入；检查板卡的 8554/TCP。 |
| 页面没有实时画面 | 确认 Windows 的 FFmpeg 仍在推流；部署机能否访问 Atlas 的 RTSP 地址；确认浏览器可访问 UDP/8189。 |
| 模型安装失败 | 查看 agent 日志和 model status；确认 OM、manifest 与板卡 CANN/SoC 匹配。 |

查看云端服务状态和日志：

```sh
cd project/src/services
docker compose --env-file .env ps
docker compose --env-file .env logs --tail 100
```

查看 Atlas 服务日志：

```sh
sudo journalctl -u yunshao-agent.service -n 50 --no-pager
sudo journalctl -u yunshao-classroom-runtime.service -n 50 --no-pager
```

## 9. 当前局域网演示部署（2026-10-22）

本机平台入口为 `https://172.20.10.4:19080/`，Atlas 为 `172.20.10.2`，演示网段为 `172.20.10.0/28`。浏览器使用 HTTPS；HTTP `18080` 跳转到 HTTPS `19080`。演示账号为 `lan_demo`，密码保存在部署机本地、已被 Git 忽略的 `.secret-lan-access.json` 中。客户端首次访问时需信任部署机本地配置目录中的 `yunshao-lan-ca.crt`；只分发公开 CA 证书，不分发私钥。将环境变量 `YUNSHAO_LAN_CONFIG_DIR` 设置为该本地配置目录。

当前运行组件与数据位置：

| 部件 | 位置或入口 |
| --- | --- |
| Web、认证、设备、事件、媒体、推送、PostgreSQL、Mosquitto | Docker Desktop Compose 项目 `yunshao-local`、`yunshao-local-web` |
| 服务源码与私有配置 | `E:\Yunshao-dev\project\src\services`、`E:\Yunshao-dev\project\src\clients\web` |
| MediaMTX | `E:\视频制作\Yunshao-local-demo\project\src\services\media-service` |
| Web HTTP/HTTPS | `172.20.10.4:18080` → `127.0.0.1:80`；`172.20.10.4:19080` → `127.0.0.1:443` |
| Atlas MQTT TLS | `172.20.10.4:18883` → `127.0.0.1:8883` |
| WebRTC | 电脑 `172.20.10.4:8189/UDP`；播放器可回退到 HLS |
| Atlas RTSP | `rtsp://172.20.10.2:8554/classroom-demo` |
| Atlas 配置和凭据 | `/var/lib/yunshao/lan-config`、`/etc/yunshao/agent.env`、`/etc/yunshao/classroom-runtime.env` |

### 手动配置 Windows 局域网入口

以管理员身份打开命令提示字元。确认 `172.20.10.4` 仍是本机当前网卡地址；启用 IP Helper 服务，然后增加端口转发：

```text
sc config iphlpsvc start= auto
sc start iphlpsvc
netsh interface portproxy add v4tov4 listenaddress=172.20.10.4 listenport=18080 connectaddress=127.0.0.1 connectport=80 protocol=tcp
netsh interface portproxy add v4tov4 listenaddress=172.20.10.4 listenport=19080 connectaddress=127.0.0.1 connectport=443 protocol=tcp
netsh interface portproxy add v4tov4 listenaddress=172.20.10.4 listenport=18883 connectaddress=127.0.0.1 connectport=8883 protocol=tcp
netsh advfirewall firewall add rule name="Yunshao LAN Web 20261022" dir=in action=allow protocol=TCP localip=172.20.10.4 localport=18080,19080 remoteip=172.20.10.0/28 profile=any
netsh advfirewall firewall add rule name="Yunshao Atlas MQTT 20261022" dir=in action=allow protocol=TCP localip=172.20.10.4 localport=18883 remoteip=172.20.10.2 profile=any
netsh advfirewall firewall add rule name="Yunshao LAN WebRTC 20261022" dir=in action=allow protocol=UDP localip=172.20.10.4 localport=8189 remoteip=172.20.10.0/28 profile=any
netsh interface portproxy show all
```

更换网段或地址时，同步更新端口转发、防火墙来源范围、Web Origin、证书 SAN、MediaMTX 配置与 Atlas 连接地址。不要开放 PostgreSQL、Go 服务端口或 MediaMTX 管理 API。

撤销这些本机入口时，在管理员命令提示字元中执行：

```text
netsh interface portproxy delete v4tov4 listenaddress=172.20.10.4 listenport=18080 protocol=tcp
netsh interface portproxy delete v4tov4 listenaddress=172.20.10.4 listenport=19080 protocol=tcp
netsh interface portproxy delete v4tov4 listenaddress=172.20.10.4 listenport=18883 protocol=tcp
netsh advfirewall firewall delete rule name="Yunshao LAN Web 20261022"
netsh advfirewall firewall delete rule name="Yunshao Atlas MQTT 20261022"
netsh advfirewall firewall delete rule name="Yunshao LAN WebRTC 20261022"
```

### 手动启动和停止演示

先启动 Docker Desktop。在后端目录执行：

```text
cd /d E:\Yunshao-dev\project\src\services
docker compose -f docker-compose.yaml -f "%YUNSHAO_LAN_CONFIG_DIR%\lan-push.compose.yaml" up -d --no-build
```

再启动 Web，并重启 Web 容器使其重新解析后端地址：

```text
cd /d E:\Yunshao-dev\project\src\clients\web
docker compose up -d --no-build
docker compose restart web
```

在 MediaMTX 目录单独启动 `mediamtx.exe mediamtx.yml`，保持该进程运行。测试推流时，从仓库根目录运行：

```text
ffmpeg -nostdin -hide_banner -loglevel warning -re -stream_loop -1 -i video/classroom_behavior.mp4 -map 0:v:0 -vf "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2:black,fps=15" -an -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p -g 30 -bf 0 -b:v 700k -f mpegts "udp://172.20.10.2:5000?pkt_size=1316"
```

不需要测试视频时跳过 FFmpeg。停止推流按 `Ctrl+C`；停止服务但保留数据库和运行数据：

```text
cd /d E:\Yunshao-dev\project\src\clients\web
docker compose stop
cd /d E:\Yunshao-dev\project\src\services
docker compose stop
```

Atlas 服务已配置为随板卡启动；需停止时在 Atlas 上执行 `sudo systemctl stop yunshao-classroom-runtime.service yunshao-agent.service yunshao-mediamtx.service`。日常停止不要运行 `docker compose down -v`，以免删除数据库和事件证据卷。

### 推送服务 DNS 覆盖

当前 Clash DNS 对部分浏览器推送域名返回 Fake-IP（`198.18.0.0/15`），推送服务会拒绝这些地址。新增推送订阅后，从服务目录查询启用的域名：

```text
docker exec yunshao-local-postgres-1 psql -U yunshao_bootstrap -d yunshao -At -c "select distinct substring(endpoint from '^https://([^/]+)') from push.subscriptions where disabled_at is null;"
```

对标准推送域名（`notify.windows.com`、`fcm.googleapis.com`、`push.services.mozilla.com`、`web.push.apple.com`）使用可信的 HTTPS DNS 查询服务取得公网 A 记录，并手动更新 `%YUNSHAO_LAN_CONFIG_DIR%\lan-push.compose.yaml`。格式如下，地址应替换成当时查到的公网 IPv4：

```yaml
services:
  push-service:
    extra_hosts:
      - "fcm.googleapis.com:142.250.72.170"
```

只重建推送服务应用覆盖配置：

```text
cd /d E:\Yunshao-dev\project\src\services
docker compose -f docker-compose.yaml -f "%YUNSHAO_LAN_CONFIG_DIR%\lan-push.compose.yaml" up -d --no-build --no-deps push-service
```

这些地址是 DNS 快照，服务商变更地址后需重新查询。长期部署应让容器使用能返回真实公网地址的 DNS。当前演示验收证据见 `project/docs/Specification/platform_deployment/evidence/lan_20261022.json`；本机截图与本地凭据不提交仓库。

## 10. 文档渲染与 KooCLI 辅助流程

文档封面、截图和图表流程在 Windows 上统一通过 Git Bash 运行仓库内的 `codearts_proof/skills/huawei-cloud-publish-work-to-gallery/scripts/preflight.sh`。该门禁检查字体、Python 渲染依赖和 Playwright；缺依赖时按脚本提示安装，必要时下载 Chromium。预检通过后会在系统临时目录写 `font-gate-ok` 标记。KooCLI 的安装和凭据配置见本指南“华为云 KooCLI（hcloud）”一节。

日常停止不要使用 docker compose down -v；该命令会删除数据库和事件证据卷。
