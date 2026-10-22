# Windows GPU 环境

WSL 当前无法连接外网时，可以用独立 Python 3.12 虚拟环境在 Windows GPU 上运行训练和 ONNX 测试。此环境只安装到本目录的 `.venv/`，不会修改 Conda base 环境；`.venv/` 已加入 `.gitignore`。

在模型目录运行：

```powershell
uv venv .venv --python 3.12
uv pip install --python .venv\Scripts\python.exe --index-url https://download.pytorch.org/whl/cu124 torch==2.5.1 torchvision==0.20.1
uv pip install --python .venv\Scripts\python.exe -r requirements-windows-cu124-lock.txt
```

Roboflow API key 保存在本机用户级 `ROBOFLOW_API_KEY` 环境变量中，不写进仓库或脚本。新启动的 PowerShell 才会自动继承该变量；如果终端或开发工具已经打开，则先从用户级变量载入当前进程，再执行下载：

```powershell
$env:ROBOFLOW_API_KEY = [Environment]::GetEnvironmentVariable('ROBOFLOW_API_KEY', 'User')
.venv\Scripts\python.exe model_train\1-download-dataset.py
.venv\Scripts\python.exe model_train\2-analyse-dataset.py
.venv\Scripts\python.exe model_train\3-train-models.py --datasets classroom_behavior --epochs 100 --imgsz 640 --batch 8 --device 0 --workers 4 --seed 42 --deterministic --name yolo11n
Remove-Item Env:\ROBOFLOW_API_KEY
```

Windows GPU 的 CUDA 轮子面向本机 NVIDIA 驱动；板端 CANN/ATC/ACL 仍只在 Atlas 200I DK A2 上运行。若本机 CUDA 无法初始化，先检查 NVIDIA 驱动，再按项目 `environment.yml` 在联网的 WSL 开发环境中建立 Conda 环境。
