# 仓储检测：训练阶段 SOP

## 目的与固定输入

本阶段复现 YOLO11n 仓储物品检测模型。固定数据源为 Roboflow workspace warehouse-recognition、project warehouse-detection-xtbsn、version 1，导出格式为 YOLOv11。训练前必须确认当前数据版本及类别映射，不能只依赖旧的报告数字。

## 环境与执行目录

在 WSL/Linux 使用仓库提供的 yunshao Conda 环境，并从此模型目录运行命令：

```bash
cd project/preparation/model_warehouse_detection
conda env update -n yunshao -f environment.yml
conda activate yunshao
export ROBOFLOW_API_KEY='由运维安全注入的 Roboflow API key'
```

Roboflow API key 不得写入仓库或训练报告。把 CUDA、GPU、Python 和主要依赖版本记入实验记录。

## 复现步骤

1. 下载固定 v1 数据并分析标签：

```bash
python model_train/1-download-dataset.py
python model_train/2-analyse-dataset.py
```

检查 model_train/dataset_analysis/warehouse_detection/ 的各 split 图片数、类别频次、非法标注和可视化样本。历史记录中曾有分割标注混入检测标签、评估跳过图片的情况；本次必须报告实际参与评估的图片数和跳过原因。

2. 运行确定性 YOLO11n 训练：

```bash
python model_train/3-train-models.py --datasets warehouse_detection --epochs 100 --imgsz 640 --batch 16 --device 0 --workers 8 --seed 42 --deterministic --name yolo11n
```

运行输出在 model_train/runs/train/yolo11n_warehouse_detection/；训练内置的验证使用 val split，不替代 test。

3. 将本次运行的 checkpoint 固定到导出/评估脚本使用的路径：

```bash
cp model_train/runs/train/yolo11n_warehouse_detection/weights/best.pt model_train/train_result/yolo11n_warehouse_detection_best.pt
cp model_train/runs/train/yolo11n_warehouse_detection/weights/last.pt model_train/train_result/yolo11n_warehouse_detection_last.pt
```

4. 对留出 test split 做 PyTorch 验证：

```bash
python model_train/4-validate-model.py --datasets warehouse_detection --split test --checkpoint best --imgsz 640 --batch 16 --device 0
```

## 产物与复现记录

- 数据和 data.yaml：model_train/datasets/warehouse_detection/；
- 分析结果及图片：model_train/dataset_analysis/warehouse_detection/；
- 训练运行、原始 best/last：model_train/runs/train/yolo11n_warehouse_detection/；
- 后续阶段使用的固定权重：model_train/train_result/yolo11n_warehouse_detection_best.pt 和 last.pt；
- PyTorch 验证：model_train/runs/validation/ 下的 timestamped JSON、CSV 和图表。

保存代码提交号、数据集版本、YAML 与图像/标注清单哈希、训练参数、GPU/驱动、Python/torch/ultralytics 版本、checkpoint SHA-256、实际评估图片/实例数量及异常说明。历史报告不得混作本次复现结论。
