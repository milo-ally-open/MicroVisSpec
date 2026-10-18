# 课堂行为检测模型工作流

本目录负责课堂行为检测模型。首版任务为单模型、多类别目标检测：识别 `hand-raising`、`reading`、`writing`。模型只输出行为类别、边界框和置信度，不推断学生身份、成绩或“专注度”。

## 数据来源与准入

- 固定候选数据集：Roboflow Universe `main-vdoys/student-dataset-demo` v1，约 4,739 张图像，页面列出的类别为 `reading`、`hand-raising`、`writing`，页面标记 Public Domain。
- 来源页：[student-dataset-demo](https://universe.roboflow.com/main-vdoys/student-dataset-demo)。下载后保留来源和许可证说明，并核对实际 `data.yaml`、类别顺序、图片/标签数量、损坏文件、类别分布和样例框。页面信息不能替代本地数据验收。
- 下载需要 `ROBOFLOW_API_KEY` 环境变量。密钥只从环境变量读取，不能写入仓库。
- 训练数据、生成的权重和运行产物不提交 Git；见本目录 `.gitignore`。
- 如后续使用现场拍摄的课堂数据，须先取得授权并限制访问；不做人脸识别或学生身份跟踪。

## 环境

优先使用仓库约定的 WSL Conda `yunshao` 环境。若本机环境尚不存在，创建一次；之后用 `env update` 对齐依赖：

```bash
conda env create -n yunshao -f environment.yml  # only when the environment is missing
conda env update -n yunshao -f environment.yml # when it already exists
conda activate yunshao
```

WSL 无法联网时可使用本目录的 [`WINDOWS_GPU_SETUP.md`](WINDOWS_GPU_SETUP.md) 准备隔离的 Windows GPU 虚拟环境；模型目录和命令参数保持一致，Atlas 板端步骤仍在开发板上执行。

## 研发步骤

以下命令均在本目录运行。首轮先下载、审查和分析数据；只有数据验收通过后才训练。

### 1. 下载固定版本

```powershell
$env:ROBOFLOW_API_KEY = [Environment]::GetEnvironmentVariable('ROBOFLOW_API_KEY', 'User')
if (-not $env:ROBOFLOW_API_KEY) { throw 'Set ROBOFLOW_API_KEY in the Windows user environment first.' }
.venv\Scripts\python.exe model_train\1-download-dataset.py
Remove-Item Env:\ROBOFLOW_API_KEY
```

数据写入 `model_train/datasets/classroom_behavior/`，不提交到 Git。

### 2. 核验并分析数据

```bash
python model_train/2-analyse-dataset.py
```

检查 `model_train/dataset_analysis/classroom_behavior/` 中的类别统计、划分统计、框分布、样例、`image_issues.csv` 和 `cross_split_exact_duplicates.csv`。检查损坏图片；核对缺失标签是否确实是有意保留的空图；清理跨划分完全重复的图像；再确认类别名称与映射。SHA-256 重复检查只能发现字节完全相同的图片；还须查看样例，排查同一课堂/视频的近邻帧或重新编码图像跨训练、验证、测试集。发现问题时先清理或重新划分数据，不用训练指标掩盖数据泄漏。

### 3. 训练 YOLO11n

```bash
python model_train/3-train-models.py --datasets classroom_behavior --epochs 100 --imgsz 640 --batch 8 --device 0 --workers 4 --seed 42 --deterministic --name yolo11n
```

训练运行写入 `model_train/runs/train/yolo11n_classroom_behavior/`。若显存不足，降低 batch 并在报告中记录实际参数。模型权重固定复制到 `model_train/train_result/yolo11n_classroom_behavior_best.pt`。

### 4. 独立测试集评估

```bash
python model_train/4-validate-model.py --datasets classroom_behavior --split test --checkpoint best --imgsz 640 --batch 8 --device 0
```

报告写入 `model_train/runs/validation/`。报告总体和逐类别 Precision、Recall、mAP、推理耗时与权重 SHA-256。`reading` 和 `writing` 的视觉语义可能相近，要重点检查混淆矩阵和远景误检/漏检。第三方模型页面的指标不是本模型的评估结果。

### 5. 导出 ONNX

```bash
python model_export/export_onnx.py --datasets classroom_behavior --checkpoint best --imgsz 640 --device cpu --opset 17
```

ONNX 与来源清单写入 `model_export/artifacts/`。

### 6. Atlas 200I DK A2 端侧验证

按 `../model_vehicle_detection/WORKFLOW.md` 中已有的 ATC、精度/性能/内存评估和视频 demo 步骤执行。产物和报告放在本模型目录下。ONNX/OM 需要在相同测试集上比较，视频演示不能替代测试集评估。

## 完成条件

- [x] 数据来源、版本、许可证、类别顺序和划分已记录；所有图片可读、标签完整、跨划分字节级重复为 0。近邻视频帧/重编码图片重复尚未核验。
- [x] 新训练的权重经独立 test 集评估，保存总体与逐类别报告、混淆矩阵和样例。
- [x] ONNX 导出成功，记录权重和 ONNX 哈希，并在同一 test split 上完成 ONNX 精度核对。
- [ ] Atlas 200I DK A2 的 OM 精度、性能、内存和视频运行结果均有实测证据；模拟或桌面结果不能标成板端实测。

本轮训练、评估和转换结果见 [`RESULTS.md`](RESULTS.md)。
