# 课堂行为检测：YOLO11n 复现流程

本流程在 Linux WSL 的 Conda `yunshao` 环境中运行，使用 Roboflow Universe `main-vdoys/student-dataset-demo` v1，训练 `hand-raising`、`reading`、`writing` 三类目标检测模型。训练代码、数据、运行记录、权重和导出产物都放在本模型目录内。数据集与权重不提交 Git。

## 环境

复用 WSL 中的 Conda `yunshao` 环境，不创建虚拟环境：

```bash
conda env update -n yunshao -f environment.yml
conda activate yunshao
```

## 运行

以下命令在本目录执行。下载密钥只通过环境变量提供：

```bash
export ROBOFLOW_API_KEY='你的 Roboflow API key'
python model_train/1-download-dataset.py
python model_train/2-analyse-dataset.py
```

先检查 `model_train/dataset_analysis/classroom_behavior/dataset_summary.json` 的类别顺序、各划分样本数、缺失标签、非法标签和跨划分重复，再开始训练：

```bash
python model_train/3-train-model.py --epochs 100 --imgsz 640 --batch 8 --device 0 --workers 4 --seed 42
```

模型从 Ultralytics `yolo11n.pt` 预训练权重开始训练，启用确定性训练，输出到 `model_train/runs/train/`。best/last 权重及含训练参数、时间和 SHA-256 的记录写入 `model_train/train_result/`。训练默认不覆盖已有运行目录。

独立测试集评估：

```bash
python model_train/4-validate-model.py --datasets classroom_behavior --split test --checkpoint best --imgsz 640 --batch 8 --device 0
```

导出固定 640 输入的 ONNX：

```bash
python model_export/export_onnx.py --model-name yolo11n --datasets classroom_behavior --checkpoint best --imgsz 640 --device cpu --opset 17
```

## 复现边界

流程固定了数据集项目与版本、模型系列、默认训练参数和随机种子。首次训练时 Ultralytics 会下载 `yolo11n.pt`；运行记录会保存训练参数及输出权重哈希。Roboflow API Key 和数据集文件由使用者提供，不写入仓库。首次训练完成后，best 权重才会出现在 `model_train/train_result/`；本次代码整理没有启动训练。

模型类别只描述边界框内的可见行为，不推断学生身份、成绩或专注度。真实课堂数据应取得授权并限制访问。
