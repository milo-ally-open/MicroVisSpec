# 井盖检测：训练阶段 SOP

## 目的与固定输入

本阶段从 Roboflow YOLO 数据集生成 YOLO11n/640 检测权重。数据源为按固定筛选规则选定的 Roboflow 井盖检测公开数据集（workspace/project/version 与类别清单由 1-download-dataset.py 内固化常量定义，下载时逐项校验）。任何数据版本或类别映射变化都应作为新的实验记录，不得覆盖成同一次复现。

输入包括本目录的下载、分析、训练和验证脚本，以及仓库提供的 Conda environment.yml。当前工作区中的数据目录可能不完整；下载完成后必须检查标签和 split，再开始训练。

## 环境与执行目录

在 WSL/Linux 仓库根目录开始，先进入模型目录。environment.yml 位于该目录，后续命令也都从这里执行：

```bash
cd project/preparation/model_manhole_cover
conda env update -n yunshao -f environment.yml
conda activate yunshao
```

Roboflow 凭据仅从环境变量读取，不写入文件、命令历史或报告：

```bash
export ROBOFLOW_API_KEY='由运维安全注入的 Roboflow API key'
```

## 复现步骤

1. 下载固定数据集版本：

```bash
python model_train/1-download-dataset.py
```

2. 生成数据质量分析：

```bash
python model_train/2-analyse-dataset.py
```

检查 model_train/dataset_analysis/manhole_cover/dataset_summary.json 以及图表。确认 train、valid、test 图片目录和对应 labels 完整，类别 ID 与 data.yaml 的 names 一致，无非法或空目录。当前已知某些工作区快照曾缺少 test 标签；遇到缺失须重新下载，不要继续训练或报告完整精度。

3. 从 Ultralytics YOLO11n 预训练权重训练：

```bash
python model_train/3-train-model.py --epochs 100 --imgsz 640 --batch 8 --device 0 --workers 4 --seed 42 --run-name yolo11n_manhole_cover
```

脚本固定 deterministic=true、patience=30，并拒绝覆盖已有运行目录（exist_ok=False）。`--run-name` 为本场景 SOP 的显式适配参数：训练运行目录与 train_result 产物命名均由其派生，用于唯一 run-name 与失败重试隔离；省略时默认 `yolo11n_manhole_cover`，行为与存量骨架一致。若使用 CPU，需显式把 device 改成 cpu，并将其记录为不同实验。

4. 使用独立 test split 验证 best checkpoint：

```bash
python model_train/4-validate-model.py --datasets manhole_cover --split test --checkpoint best --imgsz 640 --batch 8 --device 0
```

只有指定 test 的结果可记录为留出集结果；不得用训练日志中的 val 指标替代 test。

## 产物与追溯记录

- 下载数据：model_train/datasets/manhole_cover/，含 data.yaml、图像和 YOLO labels。
- 分析摘要：model_train/dataset_analysis/manhole_cover/。
- 训练完整运行：model_train/runs/train/<run-name>/，包含 Ultralytics 曲线、日志和原始 checkpoint。
- 固定权重：model_train/train_result/<run-name>_best.pt、<run-name>_last.pt，以及 <run-name>_training.json。
- PyTorch 验证：model_train/runs/validation/ 下的 timestamped 目录、evaluation.json、report.csv 和 Ultralytics 图表。

每次复现归档代码提交号、数据集 version、data.yaml 与图片/标签清单哈希、训练命令、GPU/驱动、Python/torch/ultralytics 版本、训练记录和 best 权重 SHA-256。不要把只在验证 split 上取得的结果标成盲测。