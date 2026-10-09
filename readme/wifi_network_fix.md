# Atlas 200I DK A2 板载 AX210 无线网卡排查与修复记录

> 维护记录：2026-10-09。后续只要驱动、固件、内核或联网状态有变化，继续更新本文件。修复工程位于 `project/patches/atlas200idk-a2-ax210/`，当前 Git 分支为 `dev`。

## 当前结论

AX210 现已可以被板端 Linux 驱动并扫描附近 Wi-Fi。实测条件与结果：

- 板端：Ubuntu 22.04、aarch64、内核 `5.10.0+`。
- PCI：Intel AX210，设备 ID `8086:2725`、子系统 `8086:0024`，位于 `0001:09:00.0`。
- 模块：`iwlwifi` 和 `iwlmvm` 已成功加载；`lspci -nnk` 显示 `Kernel driver in use: iwlwifi`。
- 固件：内核日志显示加载 `59.601f3a66.0 ty-a0-gf-a0-59.ucode`，并识别 `Intel(R) Wi-Fi 6 AX210 160MHz`。
- 接口：驱动创建 `wlan0`，sysfs 下有 `phy0`。
- 射频：最初 `rfkill` 显示 WLAN `Soft blocked: yes`；解除后软、硬阻断都为 `no`。
- 扫描：`iw dev wlan0 scan` 返回多个 BSS，包含 2.4 GHz 和 5 GHz 信号，也发现了手机热点。

因此，当前“找不到 Wi-Fi 信号”的障碍已排除。用户随后反馈已通过 AX210 连接上 Wi-Fi。板端驱动和扫描已有实测；本轮没有重新读取连接后的 IP、默认路由或外网访问结果。

## 起因与排查证据

重装到 23.0.RC3 镜像后，板子能通过 PCIe 识别 AX210，但系统没有可用的 Intel 无线驱动和固件：

```text
Kernel: 5.10.0+ / aarch64
PCI: 8086:2725, subsystem 8086:0024
modinfo iwlwifi: Module iwlwifi not found
/lib/firmware 中 iwlwifi 文件：0
NetworkManager/网络接口中没有无线设备
```

这不是 Wi-Fi 密码或手机热点名称的问题。`nmcli radio` 的硬件/软件开关显示 enabled，只代表无线开关状态，不会补齐缺失的内核驱动和固件。随后 AX210 驱动加载后，`rfkill` 又揭示出一个独立问题：新出现的无线 phy 被软件阻断，需要显式 `rfkill unblock wifi`。

## 源码和版本匹配

用户提供的官方 SDK 包放在仓库 `readme/Ascend-hdk-310b-sdk-soc_23.0.rc3.zip`，大小约 541 MiB，SHA-256：

```text
e64b9cf9e1f1af3ab9f00465a9754fbf56ef120e6a2e9073d531e0d69c42729d
```

从包中核对到：

- `Ascend310B-source.tar.gz` 内核 `Makefile` 标记 Linux `5.10.0`，与板端 `5.10.0+` 基线吻合；源码 PCI 表含 `2725:0024` AX210 条目。
- SDK 工具链为 Huawei Do-Compiler GCC 7.3.0；构建使用板端导出的配置快照，未拿普通 Ubuntu 的 ARM64 `.ko` 代替。
- 板端 `/proc/config.gz` 保存于修复工程 `artifacts/ax210-board-config-5.10.0+.config.gz`。
- 配置快照显示 `CONFIG_MODVERSIONS` 未启用。板端自带的 `cfg80211`、`mac80211` 模块与运行内核 vermagic 一致，且已被 `rtlwifi` 等模块使用，所以修复没有卸载或覆盖它们。
- SDK 源码默认会为 iwlwifi 编译可选 LED 支持；而板端正在用的 `mac80211` 没有导出 `__ieee80211_get_radio_led_name`。第一版 `iwlmvm` 因此报告 `Unknown symbol`。工程补丁把 `IWLWIFI_LEDS` 默认值改为 `n`，再构建后不再引用该符号。无线 LED 仅为可选功能，不参与扫描/联网。

## 固件选择

SDK 5.10.0 的 AX210 驱动将固件 API 上限设为 59，因此安装的文件名必须是：

```text
iwlwifi-ty-a0-gf-a0-59.ucode
```

文件从 Ubuntu `linux-firmware-intel-wireless` 20240318 包提取，并解压为内核固件加载器可直接读取的 `.ucode`（板端 `CONFIG_FW_LOADER_COMPRESS` 未启用）。文件 SHA-256：

```text
7620861bcd60e5e89a2f0571b98e67f9451c52ce6c912b46c2f141b0005edd6c
```

不要将新固件 API 77/89 文件简单改名成 API 59。

## 构建、安装和扫描流程

完整脚本、补丁、配置快照和当前可用构建产物都在 [`project/patches/atlas200idk-a2-ax210/`](../project/patches/atlas200idk-a2-ax210/README.md)。核心流程：

1. 在 WSL Ubuntu 安装 `build-essential bc bison flex libssl-dev libelf-dev zstd`。
2. 运行 `scripts/build.sh`：解出 SDK 的 Linux 5.10 源码和 ARM64 工具链，应用板端配置及 LED 兼容补丁，构建 `iwlwifi.ko`、`iwlmvm.ko`。
3. 运行 `scripts/fetch-firmware.sh`：取出 API 59 AX210 固件。
4. 把 `iwlwifi.ko`、`iwlmvm.ko`、`.ucode` 和 `install-board.sh` 传到板端，运行安装脚本。模块放在 `/lib/modules/5.10.0+/extra/ax210/`，固件放在 `/lib/firmware/`，随后 `depmod -a` 和加载驱动。
5. `rfkill unblock wifi`、`ip link set wlan0 up`，用 `iw dev wlan0 scan` 扫描。

板端原本缺少 `iw`、`rfkill`。尝试 `apt-get install iw rfkill` 时，板端 DNS 无法解析 `repo.huaweicloud.com`，没有改动 APT 源；改为从 Ubuntu Ports 下载匹配 Ubuntu 22.04 arm64 的两个小 `.deb` 并本地安装。两个 `.deb` 及 SHA-256 也放在修复工程 `artifacts/` 中。没有执行 `apt upgrade`。

## 本次实际遇到的错误与解决

| 阶段 | 现象 | 处理 |
|---|---|---|
| 初始检查 | PCI 看得到 AX210，但 `iwlwifi` 不存在、没有固件和无线接口 | 从匹配版本的 Ascend SDK 内核源码构建模块，并补齐 AX210 API 59 固件 |
| 首次加载 | `iwlmvm: Unknown symbol __ieee80211_get_radio_led_name` | 板载 `mac80211` 未导出可选 LED 符号；禁用 iwlwifi LED 功能后重编，仅安装 iwlwifi 模块 |
| 固件初始化 | 日志提示 `api flags index 2 larger than supported by driver` | 同一启动日志继续显示 API 59 固件已加载、AX210 已识别；最终扫描成功，该提示不阻止本驱动与固件工作 |
| 扫描前 | `/sys/class/rfkill/rfkill0/soft=1`，且没有 `iw` 命令 | 安装 `iw`、`rfkill`，执行 `rfkill unblock wifi` 并启用 `wlan0` |
| 安装诊断工具 | 板端 APT 下载 DNS 失败 | 从 Ubuntu Ports 下载 arm64 `.deb`，检查依赖后本地 `dpkg -i`；未升级系统 |

## 复查命令

```bash
uname -r
lspci -nnk -s 0001:09:00.0
modinfo iwlwifi | grep -E '^(filename|firmware|depends|vermagic):'
lsmod | grep -E '^(iwlwifi|iwlmvm|cfg80211|mac80211)\b'
rfkill list wifi
ip -br link show wlan0
iw dev wlan0 scan
```

快速扫描并只显示 BSS、频率、信号强度和 SSID：

```bash
rfkill unblock wifi
ip link set wlan0 up
iw dev wlan0 scan | awk '/^BSS / {print; next} /^[[:space:]]+(freq|signal|SSID):/ {print}'
```

## 影响范围与回滚

- 没有替换内核镜像、没有执行全系统升级，也没有改 CANN/ACL/ATC/NPU 驱动文件。
- 新模块独立放在 `extra/ax210`；固件只增加 API 59 文件。`iw`/`rfkill` 为扫描与开关诊断工具。
- 修复工程提供 `scripts/uninstall-board.sh`；它只卸载本工程模块/固件并恢复安装前备份，不会卸载共享的 `cfg80211`、`mac80211`。

## 变更记录

- 2026-10-09：确认 SDK 包与板端内核 5.10.0 基线匹配；导出当前配置；完成 ARM64 模块构建与 API 59 固件部署；修复可选 LED 符号冲突；安装 AX210；实际扫描到多个附近 Wi-Fi 信号。完整过程和可复现工程已同步到 `project/patches/atlas200idk-a2-ax210/`。
- 2026-10-09：从全新 WSL 构建目录再次运行工程脚本成功；将重建产物安装到板端，`verify-board.sh` 成功，并再次扫到附近 BSS。板端已安装文件的 SHA-256 与工程 `artifacts/SHA256SUMS` 一致。
- 2026-10-09：用户反馈开发板已连接上 Wi-Fi。记录为连接成功；IP、路由和外网连通性尚未在板端复核。
