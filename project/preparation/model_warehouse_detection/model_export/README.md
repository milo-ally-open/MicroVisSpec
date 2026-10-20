# 仓储检测：ONNX 导出阶段 SOP

## 目的与固定参数

将仓储检测 YOLO11n best checkpoint 转成标准 ONNX，供桌面 ONNX Runtime 评估和 Atlas ATC 转换。固定 batch 1、输入 640×640、opset 17；默认静态形状。

## 前置条件

确认 model_train/train_result/yolo11n_warehouse_detection_best.pt 是本次要复现的权重，并记录 SHA-256。确认 data.yaml 的类别名称和顺序与训练权重一致。命令从模型目录根运行：

```bash
conda activate yunshao
cd project/preparation/model_warehouse_detection
```

## 导出命令

```bash
python model_export/export_onnx.py --datasets warehouse_detection --checkpoint best --imgsz 640 --device cpu --opset 17
```

若权重留在原始 runs/train 目录，脚本会尝试从 model_train/runs/train/yolo11n_warehouse_detection/weights/best.pt 查找。为了清晰区分不同实验，推荐先按 model_train SOP 固定权重文件名。

## 输出与验收记录

输出为 model_export/artifacts/yolo11n_warehouse_detection_best.onnx 和 export_manifest.json。Manifest 保存输入 checkpoint、哈希、导出参数与运行库版本。

归档代码提交号、命令、工具版本、checkpoint 和 ONNX SHA-256。后续转换必须使用此 ONNX；转换结果的设备兼容和精度差异需在 model_convert/model_val 阶段分别验收。
