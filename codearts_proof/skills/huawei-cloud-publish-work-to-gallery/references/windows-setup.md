# Windows 前置准备（PowerShell 5.1 / Git Bash）

本文件承接 SKILL.md 中的 Windows 专属一次性准备项，首次在 Windows 上发布前按此完成即可。

## 0. Start-Process 后台启动注意（Windows 上启动本地服务/隧道）

`Start-Process -RedirectStandardOutput X -RedirectStandardError X` **不能把 stdout 与 stderr 重定向到同一文件**（PS 5.1 抛 `InvalidOperationException`，且重定向到同一路径时不可见）。正确做法是拆成两个日志文件：

```powershell
$out = "<log-dir>\http-out.log"; $err = "<log-dir>\http-err.log"
Start-Process -FilePath "python" -ArgumentList "-m","http.server","8080","--bind","127.0.0.1" `
  -WorkingDirectory "<workDir>" -RedirectStandardOutput $out -RedirectStandardError $err -WindowStyle Hidden
# 注意：-WorkingDirectory 与 -RedirectStandardOutput 参数在较长命令行组合时可能触发外壳层的
# "ChildProcess.kill" 类异常，属于 opencode/PS 外壳与 Start-Process 的子进程生命周期问题——
# 命令被打断不代表服务没起来，重新执行应优先检查端口监听（netstat / Get-NetTCPConnection）再判断是否重试。
```

启动后**必须验证进程真实监听端口并返回本项目内容**（见 SKILL.md Step 2 健康检查要求），不要假设已成功。

## 0b. Git Bash 下运行脚本必须禁用 MSYS 路径转换

**现象（实测 2026-09）：** 在 Git Bash 中直接执行 `node $SKILL_DIR/scripts/api.mjs GET /v1/gallery/announcements/current`，api.mjs 报
`Request path contains unescaped characters`。根因是 Git Bash 的 MSYS 会把以 `/` 开头的参数当作 POSIX 路径自动转换成 Windows 路径（如 `/v1/...` → `C:/Program Files/Git/v1/...`），path 含空格/盘符前缀即触发 undici 校验失败。**症状与 api.mjs 无关，对任何以 `/` 开头的参数脚本（api.mjs/check-version.mjs）均适用。**

**修复：** 调用前设置一次即可（本会话内全部生效，子进程自动继承）：

```bash
export MSYS_NO_PATHCONV=1
```

PowerShell 环境无此问题（仅 Git Bash 需要）。若不便改环境，也可临时写作 `MSYS_NO_PATHCONV=1 node $SKILL_DIR/scripts/api.mjs ...`（单条命令前缀式）。

## 1. 控制台编码（PowerShell 5.1）

PowerShell 5.1 控制台默认按系统代码页（GBK/CP936）解码 stdout，导致 Node/Python 脚本输出的中文显示为乱码。脚本内部已加 `setDefaultEncoding("utf8")` / `sys.stdout.reconfigure(encoding="utf-8")`（win32 条件），但作为双保险，首次进入发布流程时执行一次：

```powershell
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::InputEncoding  = [System.Text.Encoding]::UTF8
$env:PYTHONIOENCODING     = "utf-8"
chcp 65001 > $null
```

## 2. git 可用性

`git --version` 失败（未安装）时，不要跳过 Step 3，按以下引导用户安装：

```powershell
# 从清华镜像下载 Git for Windows
# 1. 打开 https://mirrors.tuna.tsinghua.edu.cn/github-release/git-for-windows/git/
# 2. 进入最新版本目录（如 Release-2.46.0/），下载 64-bit 安装包（如 Git-2.46.0-64-bit.exe）
# 3. 运行安装程序，保持默认选项（安装时自动配置 PATH）
# 4. 安装完成后重新打开终端，再次运行 git --version 验证
# 安装 Git for Windows 会附带 Git Bash（C:\Program Files\Git\bin\bash.exe）
```

## 3. 渲染门禁

Windows 统一使用 Git Bash 运行仓库内的 `scripts/preflight.sh`。Git for Windows 安装后可从 Git Bash 打开仓库目录，再执行：

```bash
bash "$SKILL_DIR/scripts/preflight.sh"
```

若该命令不可用，请安装或修复 Git for Windows 后再继续。预检通过会在系统临时目录写入 `font-gate-ok`；门禁失败时先查看同目录的 `preflight.log` 并修复原因。Windows 默认包含微软雅黑和 Segoe UI Emoji，Playwright Chromium 通常也无需额外系统 DLL。

## 4. Playwright Python 包路径（跨平台通用）

Python Playwright 通过 `pip install playwright` 安装为 Python 包，模块解析走 `sys.path`（site-packages），无需 `node_modules` 目录：

- 自写截图脚本可放在任意目录运行（Python 包全局可见），无需像 Node 那样放 `scripts/` 下。内置脚本（`screenshot_guard.py`）直接 `python $SKILL_DIR/scripts/screenshot_guard.py` 即可运行。
- `python -c "import playwright"` 可在任意 CWD 测试可用性（Python 包解析不依赖 CWD）：

```powershell
# 直接测试 Playwright Python 包是否可用（无需切换目录）
python -c "import playwright"
```

> 若 `preflight.sh` 检测到 Playwright 未装，会自动安装 `playwright` 并按需运行 `python -m playwright install chromium`；预检会优先复用系统 Edge/Chrome。

## 5. 详情 zip 打包（首选 build-detail-zip.mjs，跨平台零依赖）

**首选：** `build-detail-zip.mjs` 内置正确目录结构（README.md@根 + `resources/` 前缀）并自动跑服务端同源校验，一步到位、无编码/平台坑：

```bash
node $SKILL_DIR/scripts/build-detail-zip.mjs --readme <README.md> --resources <resources-dir> --out <workDetail.zip>
```

**备选（仅当 build-detail-zip.mjs 不可用时）：** `Compress-Archive` 在 PowerShell 5.1 会压平子目录，生成的 zip 只有 `README.md + arch.png`（丢失 `resources/` 前缀），平台校验返回 `GALLERY.PARAM.DETAIL_ZIP_INVALID`。必须用 .NET `ZipFile::CreateFromDirectory` 保留目录结构：

```powershell
Add-Type -AssemblyName System.IO.Compression.FileSystem
Remove-Item "$env:TEMP\workDetail.zip" -ErrorAction SilentlyContinue
[System.IO.Compression.ZipFile]::CreateFromDirectory("$staging", "$env:TEMP\workDetail.zip")
```

打包后可本地验证结构：`node $SKILL_DIR/scripts/validate-detail-zip.mjs <zip>`（复用服务端 `parseDetailZip` 规则）。

## 6. 发布调用（Windows 用 publish-work.mjs）

PowerShell 直接向 `api.mjs` 传中文（`workName`/`introduction`）会被按系统代码页转码 → 400 参数错误。正确做法：把发布参数写入 UTF-8 JSON 文件，再经 Node 进程内 UTF-8 调 api.mjs（`publish-work.mjs` 内部还内置了门禁强制校验）：

```bash
node $SKILL_DIR/scripts/publish-work.mjs $env:TEMP\publish-params.json --api $SKILL_DIR/scripts/api.mjs --max-age 3600
```

## 7. Python 解释器选择（`python3` Store stub 陷阱）

Windows 上 `python3` 常指向 `WindowsApps` 的 Store stub（`Get-Command python3` 有结果但执行即失败，进程退出码 9009 / 无输出）。涉及 Python 的门禁命令（`verify-glyphs.py`、matplotlib 图表等）在 Windows 上**应先确认 `python3` 真实可用，不可用时改用 `python`**：

```powershell
python3 -c "print('py3 ok')" 2>$null   # 失败（Store stub）则改用 python
python $SKILL_DIR/scripts/verify-glyphs.py <image.png>
```

`preflight.sh` 会检测 Windows 上可用的 Python 命令；若默认 `python3` 不可用，可在 Git Bash 中确认 `python --version` 并安装完整的 Python 发行版。

## 7b. 多 Python 解释器补装（Windows）

Windows 可能同时存在多个 Python 解释器，site-packages 互不可见。使用同一个 `python` 解释器运行门禁和渲染脚本，避免依赖装到另一个解释器。

`preflight.sh` 记录实际预检所用的解释器；若渲染由另一个 Python 解释器启动，请在该解释器中单独安装所需依赖。
