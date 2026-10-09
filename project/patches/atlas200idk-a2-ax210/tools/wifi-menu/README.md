# wifi-menu

AX210 / `wlan0` 的 C 语言终端 Wi-Fi 连接器，使用 NetworkManager 管理扫描和连接。界面持续留在终端中；连接期间显示状态，密码输入会遮蔽字符，NetworkManager 的密码提示通过伪终端交互，不会把密码放进进程参数。

## 使用

以 root 登录开发板后运行：

```bash
wifi-menu
```

| 按键 | 操作 |
|---|---|
| ↑ / ↓、`j` / `k` | 选择热点 |
| Enter | 连接选中的热点 |
| `r` | 重新扫描 |
| `d` | 密码提示出现前取消连接；否则断开 `wlan0` |
| Esc、Ctrl-C | 取消扫描或连接，也可在密码提示时取消 |
| `?` | 显示帮助 |
| `q` | 退出 |

已连接热点会显示绿色标记、SSID 和 IPv4 地址。界面分开显示当前连接和正在尝试连接的热点。每次连接时，工具会清除该热点配置中已保存的密码、设置“不保存密码”并关闭自动连接，然后通过 NetworkManager 请求输入密码；密码不会写入持久配置。热点列表按信号强度排序，同名 SSID 合并显示最强信号。启动和按 `r` 重扫时，后台会请求 NetworkManager 立即扫描，等待约 3.5 秒后读取结果；扫描期间界面仍可响应按键，Esc 可取消。界面只在状态或输入变化时重绘，减少终端输出和闪烁。密码输入过程中 `d` / `D` 是普通密码字符，使用 Esc 或 Ctrl-C 取消。连接失败时会尽量显示 NetworkManager 的具体错误行。

## 构建与安装

需要 GCC、NetworkManager 的 `nmcli` 和系统 `libutil`。无需 Python、ncurses、libnm 开发头文件或其他 UI 库。板端可直接构建：

```bash
make clean all
make install
```

默认安装位置为 `/usr/local/bin/wifi-menu`。Makefile 使用 `-O3`、LTO 和省略帧指针优化；运行时使用固定容量的热点列表，界面刷新不创建额外进程，只有扫描、连接及状态查询时调用系统工具。
