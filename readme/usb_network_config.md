# Atlas 200I DK A2：USB 连接电脑后联网配置记录

日期：2026-10-08（北京时间）

## 目标

电脑通过 USB Type-C/RNDIS 连接开发板，同时电脑通过 Wi-Fi 上网；开发板 USB 网卡 `usb0` 自动配置网络，能够访问公网、解析域名并通过 HTTPS 访问站点。

## 修改前状态

### 电脑侧

- Internet 上行：`WLAN`，连接 iPhone 热点，Windows 显示 `Internet`。
- 开发板 USB 网卡：Windows 适配器 `本地连接`（USB RNDIS6），地址 `192.168.137.1/24`。
- Windows Internet Connection Sharing 已启用：`WLAN` 为共享源，`本地连接` 为专用网络。

### 开发板侧

- 设备：Atlas 200I DK A2，Ubuntu 22.04，aarch64。
- 实际运行接口：`usb0 UP 192.168.137.100/24`；`eth0`、`eth1` 当时均为 DOWN。
- 原持久配置 `/etc/netplan/01-netcfg.yaml` 的关键内容：

  ```yaml
  network:
    version: 2
    renderer: networkd
    ethernets:
      eth0:
        dhcp4: yes
        nameservers:
          addresses: [8.8.8.8]
          addresses: [114.114.114.114]
      eth1:
        dhcp4: no
        addresses: [192.168.137.100/24]
        routes:
          - to: default
            via: 192.168.137.1
        nameservers:
          addresses: [8.8.8.8]
          addresses: [114.114.114.114]
      usb0:
        dhcp4: no
        addresses: [192.168.137.100/24]
  ```

- 问题：原默认路由挂在关闭的 `eth1`；活动的 `usb0` 没有网关路由和 DNS。`eth1` 与 `usb0` 还配置了同一个静态 IP。
- 初次观察到的系统日期为 2022-04-08；HTTPS 报错 `SSL certificate problem: certificate is not yet valid`。开发板安装有 `ntp` 和 `systemd-timesyncd`，后续确认 `ntp` 服务为 enabled/active。
- 修改持久配置之前，曾用运行时命令临时验证过路径：`ip route replace default via 192.168.137.1 dev usb0`，并为 `usb0` 临时指定 DNS。该临时状态不跨重启。

## 采用的配置

电脑继续通过 WLAN 提供上行，Windows ICS 的专用侧为 USB RNDIS，地址为 `192.168.137.1/24`。开发板 `usb0` 使用静态地址 `192.168.137.100/24`，默认路由经 `192.168.137.1`，DNS 优先使用 Windows ICS 地址，并配置公共 DNS 作为备用。

将 `/etc/netplan/01-netcfg.yaml` 更新为：

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    eth0:
      dhcp4: true
      dhcp4-overrides:
        route-metric: 200
      optional: true
    eth1:
      dhcp4: true
      dhcp4-overrides:
        route-metric: 200
      optional: true
    usb0:
      dhcp4: false
      addresses:
        - 192.168.137.100/24
      routes:
        - to: default
          via: 192.168.137.1
          metric: 100
      nameservers:
        addresses:
          - 192.168.137.1
          - 223.5.5.5
          - 114.114.114.114
      optional: true
```

这样 `eth0`、`eth1` 不再占用 `usb0` 的静态地址；若它们接入其他 DHCP 网络，其默认路由 metric 为 200，USB 共享路由 metric 为 100，USB 路由优先。`optional: true` 使系统启动不必等待未连接的网卡。

## 实际执行过程

1. 重新 SSH 连接开发板，并在写配置前读取接口、路由、Netplan 文件和时间服务状态。
2. 在开发板上备份原配置：

   ```bash
   cp -a /etc/netplan/01-netcfg.yaml /root/01-netcfg.yaml.backup-20261008
   ```

3. 按上方 YAML 内容更新 `/etc/netplan/01-netcfg.yaml`。
4. 生成并校验 Netplan 配置：

   ```sh
   netplan generate --debug
   ```

   命令成功返回，没有报错。

5. 应用配置：

   ```sh
   netplan apply
   ```

6. 核对 systemd-networkd 的接口状态与实际路由。`networkctl status usb0` 显示：
   - Network File：`/run/systemd/network/10-netplan-usb0.network`
   - State：`routable (configured)`
   - Address：`192.168.137.100`
   - Gateway：`192.168.137.1`
   - DNS：`192.168.137.1`、`223.5.5.5`、`114.114.114.114`

7. 为验证 USB 网络恢复，短暂禁用并重新启用 Windows `本地连接`（USB RNDIS6）适配器。适配器恢复后，ICS 仍为 WLAN → 本地连接，Windows RNDIS 地址恢复为 `192.168.137.1/24`。随后重新 SSH 登录开发板并复测。

## 完成后的状态与验证证据

- 开发板 `usb0`：UP，`192.168.137.100/24`。
- 默认路由：`default via 192.168.137.1 dev usb0 proto static metric 100`。
- 备用 DHCP 接口 `eth0`、`eth1` 的 route metric 为 200；当时两口都未连接。
- DNS：`www.baidu.com` 可解析，解析结果包含 `111.45.11.5`。
- 公网连通性：`ping -c 2 -W 2 223.5.5.5`，2 包发送、2 包收到、0% 丢包。
- HTTPS：`curl -I --max-time 15 https://www.baidu.com` 返回 `HTTP/1.1 200 OK`。
- 时间：开发板时间为 `2026-10-08`；`ntp` 服务为 enabled/active，`ntpq -pn` 有已选同步源（行首 `*`）。首次路由恢复后，HTTPS 证书校验从失败变为成功。没有手动修改系统时间，也没有改 NTP 服务配置。
- Windows USB RNDIS 适配器经历一次禁用/启用后，ICS 地址、SSH、DNS、公网 ping 和 HTTPS 均恢复正常。
- 未重启开发板，也未物理拔插 USB 线。持久性依据为 Netplan 文件、成功的 `netplan apply` 和 systemd-networkd 生成配置；本记录不声称已完成整机重启后的实测。

## 备份与回退

- 原 Netplan 备份：`/root/01-netcfg.yaml.backup-20261008`。
- 若需回退，可将该备份复制回 `/etc/netplan/01-netcfg.yaml`，再运行 `netplan generate --debug` 和 `netplan apply`。
- Windows ICS 原本已正确启用，本次没有修改 Windows 共享配置；Windows USB 适配器只做了一次短暂禁用/启用以验证恢复。

## 连接使用条件

1. Windows 电脑的 Wi-Fi/其他上行网络本身可访问 Internet。
2. Windows ICS 持续共享到 USB RNDIS6 适配器（本地连接），电脑侧保持 `192.168.137.1/24`。
3. USB Type-C 数据线连接开发板与电脑，开发板出现 `usb0`。
4. 开发板当前 USB 网络使用静态地址 `192.168.137.100/24`。如果以后修改 Windows ICS 网段或共享接口，需同步调整开发板 IP、网关和 DNS。

## 网络配置阶段未做的变更

- 未修改 CANN、编译器或 NPU 设置。
- 未保存电脑 Wi-Fi 密码、SSH 密码或其他凭据。
- 未执行开发板重启。

## 后续联网下载与 neofetch 安装验证

用户随后要求安装 `neofetch`，作为板子可联网访问软件仓库的证明；安装变更记录如下。

1. 执行 `apt-get update`。APT 从 Huawei Cloud Ubuntu Ports 仓库读取 Jammy、Jammy Security、Jammy Updates 和 Jammy Proposed 索引，传输约 34.2 MB。更新过程中，`jammy-updates/main` 和 `jammy-updates/restricted` 的 ARM64 Packages 索引分别出现 `Hash Sum mismatch`。APT 提示忽略这些未成功更新的索引并使用旧索引；更新命令以索引错误结束。
2. 查询包候选版本：

   ```sh
   apt-cache policy neofetch
   ```

   候选版本为 `7.1.0-3`，来源为 `http://repo.huaweicloud.com/ubuntu-ports jammy/universe arm64`。

3. 执行安装：

   ```sh
   apt-get install -y neofetch
   ```

   APT 成功下载 13.5 MB 软件包并完成安装；安装后显示 `No services need to be restarted`。APT 安装了 `neofetch` 和 32 个新包，并升级了 `libgs9`、`libgs9-common` 两个包；还有 436 个已升级包未处理，本次没有执行全系统升级。

4. 验证安装状态和运行结果：

   ```sh
   dpkg-query -W -f='${Status} ${Version} ${Architecture}\n' neofetch
   command -v neofetch
   neofetch --stdout
   ```

   输出确认：`install ok installed 7.1.0-3 all`，可执行文件为 `/usr/bin/neofetch`；运行结果显示 `OS: Ubuntu 22.04 LTS aarch64`、`Kernel: 5.10.0+`。这证明安装过程中板子成功从软件仓库下载了 neofetch 及其依赖。
