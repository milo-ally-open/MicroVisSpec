# Atlas 200I DK A2：AX210 驱动适配

当前阶段已验证 AX210 能加载固件、创建 `wlan0` 并扫描到周围 Wi-Fi。修复只安装两个内核模块和一份固件，没有替换板端内核镜像。

## 已确认的板端状态

| 项目 | 实测值 |
|---|---|
| 系统 | Ubuntu 22.04，aarch64 |
| 内核 | `5.10.0+`，`#1 SMP Fri Oct 20 19:51:59 CST 2023` |
| AX210 PCI ID | `8086:2725`，子系统 `8086:0024` |
| 内核源码包 | `Ascend-hdk-310b-sdk-soc_23.0.rc3.zip` |
| SDK 内核源码基线 | Linux `5.10.0`；SDK 工具链 GCC 7.3.0 |
| AX210 固件 | `iwlwifi-ty-a0-gf-a0-59.ucode`，API 59 |
| 结果 | `iwlwifi`/`iwlmvm` 已加载，PCI 已绑定，`wlan0` 已扫描到多个 2.4/5 GHz BSS |

板端运行配置中 `CONFIG_MODVERSIONS` 未启用。板上已有同一 vermagic 的 `cfg80211`、`mac80211` 模块并在使用；初次加载发现其 `mac80211` 未导出可选 LED 符号 `__ieee80211_get_radio_led_name`。补丁关闭 iwlwifi 可选 LED 支持后，避免替换这两个现有无线核心模块，AX210 驱动即可加载。

## 目录

- `patches/0001-disable-iwlwifi-leds.patch`：仅关闭可选 LED 支持，规避板端 `mac80211` 未导出的符号。
- `scripts/build.sh`：从本地 Huawei SDK ZIP 和板端保存的内核配置重建 ARM64 模块。
- `scripts/fetch-firmware.sh`：从 Ubuntu `linux-firmware-intel-wireless` 包提取 API 59 固件。
- `scripts/install-board.sh`：在板端安装模块/固件，更新 `depmod` 并加载驱动。
- `scripts/scan.sh`：解除软阻断、启用无线接口并列出附近信号。
- `scripts/uninstall-board.sh`：卸载本工程文件，若安装前已有同名文件则恢复备份。
- `scripts/verify-board.sh`：核对 PCI 绑定、模块、rfkill 和扫描结果。
- `tools/wifi-menu/`：C 语言交互式热点选择器工程，构建后安装为 `wifi-menu`。
- `artifacts/`：板端原始配置快照、已构建模块、固件及离线扫描工具包。

## 输入文件

SDK ZIP 保存在本机 `readme/assets/Ascend-hdk-310b-sdk-soc_23.0.rc3.zip`，约 541 MiB。它由用户提供，未复制进修复工程，并已由仓库 `.gitignore` 排除；重建脚本从这个本地路径读取。ZIP SHA-256：

```text
e64b9cf9e1f1af3ab9f00465a9754fbf56ef120e6a2e9073d531e0d69c42729d
```

## 从源码重建

在 WSL Ubuntu 24.04 安装构建依赖（只改 WSL，不改板端 CANN）：

```bash
sudo apt-get update
sudo apt-get install -y build-essential bc bison flex libssl-dev libelf-dev zstd
```

然后从仓库根目录执行：

```bash
bash project/patches/atlas200idk-a2-ax210/scripts/build.sh
```

脚本把 SDK 源码和临时构建树放到 WSL 家目录 `~/atlas200idk-a2-ax210/`，以 `artifacts/ax210-board-config-5.10.0+.config.gz` 为配置起点，应用项目补丁后只构建 `cfg80211`、`mac80211` 与 Intel 无线模块。输出模块 vermagic 必须为 `5.10.0+ SMP mod_unload aarch64`。

2026-10-09 已用全新 WSL 构建目录完整运行过 `build.sh`，并把这次输出通过 `install-board.sh` 重新部署到板端；`verify-board.sh` 随后确认模块加载、PCI 绑定、rfkill 已解除并能扫描到 BSS。交叉工具链没有 `objdump`，Kbuild 会打印一条工具缺失提示；子目录模块构建还提示缺少全内核 `Module.symvers`/`vmlinux`。此配置关闭 `CONFIG_MODVERSIONS`，且相关无线子目录的 `Module.symvers` 已显式传给依赖模块；实际模块加载和扫描已通过。

## 固件

```bash
bash project/patches/atlas200idk-a2-ax210/scripts/fetch-firmware.sh
```

脚本下载 Ubuntu `linux-firmware-intel-wireless` 20240318 包并解压其中 API 59 固件。此 5.10.0 驱动声明的 AX210 固件 API 上限为 59；不要把仅有 API 77/89 的新文件改名冒充 API 59。

## 安装到板端

将 `artifacts/iwlwifi.ko`、`artifacts/iwlmvm.ko`、`artifacts/iwlwifi-ty-a0-gf-a0-59.ucode` 和安装脚本传到板端同一目录。例如：

```bash
scp project/patches/atlas200idk-a2-ax210/artifacts/iwlwifi.ko \
    project/patches/atlas200idk-a2-ax210/artifacts/iwlmvm.ko \
    project/patches/atlas200idk-a2-ax210/artifacts/iwlwifi-ty-a0-gf-a0-59.ucode \
    root@192.168.0.2:/root/ax210-stage/
```

把 `scripts/install-board.sh` 也传到该目录后，在板端执行：

```bash
cd /root/ax210-stage
bash install-board.sh /root/ax210-stage
```

安装位置：

- `/lib/modules/5.10.0+/extra/ax210/{iwlwifi,iwlmvm}.ko`
- `/lib/firmware/iwlwifi-ty-a0-gf-a0-59.ucode`

脚本拒绝安装到其他内核版本，会为同名旧文件留备份，然后运行 `depmod -a` 并加载 `iwlmvm`。它不替换内核镜像。

## 扫描与验证

```bash
bash scan.sh wlan0
```

或直接运行：

```bash
rfkill unblock wifi
ip link set wlan0 up
iw dev wlan0 scan
```

## 交互式联网

板端运行：

```bash
wifi-menu
```

方向键选择扫描到的热点，Enter 连接，按 `r` 重新扫描；按 `d` 在连接中取消，或在空闲时断开。Esc、Ctrl-C 也可取消扫描或连接；按 `?` 查看帮助，按 `q` 退出。启动和重扫时，后台会请求扫描并在约 3.5 秒后读取结果；扫描期间界面仍可响应按键。当前连接与目标热点分开展示，界面只在状态或输入变化时重绘。每次连接前，工具会清除目标热点配置中保存的 PSK、关闭该配置的自动连接并设为每次询问密码；密码只用于当前连接，不保存在持久配置中。工具只管理 `wlan0`，不编辑 Netplan。

源码和详细使用说明位于 `tools/wifi-menu/`。板端自带 AArch64 GCC，可在该目录执行 `make clean all && make install` 安装到 `/usr/local/bin/wifi-menu`。默认构建使用 `-O3` 与 LTO，不依赖 ncurses、Python 或其他 UI 库；热点列表有固定上限并按信号强度排序。

查看完整状态：

```bash
bash verify-board.sh
```

当前实测 `rfkill` 的 `Soft blocked`、`Hard blocked` 均为 `no`；`iw` 扫描返回多个 BSS，频段包含 2412/2437 MHz 与 5180/5745 MHz。扫描结果中有手机热点。驱动日志的 `api flags index 2 larger than supported by driver` 是 API 59 固件携带的额外标志提示；同一启动日志继续显示固件加载、AX210 识别，扫描也成功。

## 回滚

```bash
bash uninstall-board.sh
```

脚本只卸载本工程安装的模块和固件；若安装前目标文件存在，则从 `/var/backups/ax210-repair/` 恢复。`iw`、`rfkill` 是普通用户态诊断工具，不影响 CANN；如需移除，可单独执行 `dpkg -r iw rfkill`。

## 可复现性与限制

- 板端配置快照保存为 `artifacts/ax210-board-config-5.10.0+.config.gz`，构建输出可重新生成。
- SDK、模块、固件和工具包的 SHA-256 列在 `artifacts/SHA256SUMS`。
- 当前板端 DNS 无法解析 `repo.huaweicloud.com`，所以 `apt-get install iw rfkill` 失败；本次从 Ubuntu Ports 下载了两个 arm64 `.deb`，校验依赖后本地 `dpkg -i` 安装。
- 用户反馈板端已通过 AX210 连接上 Wi-Fi。驱动、固件和扫描已在板端实测；连接后的 IP、路由和外网访问尚未重新核查。
- 模块仅对上表中的内核 `5.10.0+` 构建。更换镜像/内核后需重新取得该内核配置和匹配源码，再构建模块。
