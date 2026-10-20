# 车辆检测：训练阶段 SOP

## 目的与固定输入

本阶段复现 Roboflow Universe 的 YOLO11n 车辆检测模型。固定数据源为 workspace jey-mtuyv、project vehicle-detection-tqbaf-7cck5、version 1，导出格式为 YOLOv11。数据、模型权重和训练运行目录通常由下载/训练生成，不保证由 Git 克隆提供。

## 环境与执行目录

在 WSL/Linux 使用仓库提供的 yunshao Conda 环境，并从此模型目录运行命令：

```bash
cd project/preparation/model_vehicle_detection
conda env update -n yunshao -f environment.yml
conda activate yunshao
export ROBOFLOW_API_KEY='由运维安全注入的 Roboflow API key'
```

不要将 API key 写入仓库或训练记录。显卡、CUDA 和环境包版本应随复现实验一并记录。

## 复现步骤

1. 下载固定 v1 数据集并检查 data.yaml、类别和 split：

```bash
python model_train/1-download-dataset.py
python model_train/2-analyse-dataset.py
```

检查 model_train/dataset_analysis/vehicle_detection/ 的数据量、类别分布、非法框、标签类型和抽样可视化。若标注目录缺失或检测标签混有不适用的分割多边形，先修复/重取数据并记录来源，不要把被跳过的图片数隐藏在结果中。

2. 按固定参数训练 YOLO11n：

```bash
python model_train/3-train-models.py --datasets vehicle_detection --epochs 100 --imgsz 640 --batch 16 --device 0 --workers 8 --seed 42 --deterministic --name yolo11n
```

脚本使用仓库内的 yolo11n.pt 作为预训练初始化，运行目录为 model_train/runs/train/yolo11n_vehicle_detection。默认训练验证使用 val split；它不是最终留出 test 评估。

3. 固定权重入口。脚本原始 checkpoint 留在运行目录；把本次 best checkpoint 复制到后续脚本约定的名称：

```bash
cp model_train/runs/train/yolo11n_vehicle_detection/weights/best.pt model_train/train_result/yolo11n_vehicle_detection_best.pt
cp model_train/runs/train/yolo11n_vehicle_detection/weights/last.pt model_train/train_result/yolo11n_vehicle_detection_last.pt
```

4. 在独立 test split 上生成验证报告：

```bash
python model_train/4-validate-model.py --datasets vehicle_detection --split test --checkpoint best --imgsz 640 --batch 16 --device 0
```

## 产物与复现记录

- 固定数据集路径：model_train/datasets/vehicle_detection/；
- 数据分析：model_train/dataset_analysis/vehicle_detection/；
- 原始训练运行：model_train/runs/train/yolo11n_vehicle_detection/；
- 导出/验证脚本消费的 checkpoint：model_train/train_result/yolo11n_vehicle_detection_best.pt 和 last.pt；
- PyTorch 验证结果：model_train/runs/validation/ 下的 timestamped summary、JSON/CSV 与 Ultralytics 图表。

归档提交号、Roboflow 项目/version、data.yaml 以及图像/标签清单哈希、完整命令、GPU/驱动、Python/torch/ultralytics 版本、训练曲线和 checkpoint SHA-256。历史 train_result 文档只代表历史实验；只有重新完成本流程才能标注为本次结果。
