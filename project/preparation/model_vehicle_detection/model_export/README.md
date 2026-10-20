# 车辆检测：ONNX 导出阶段 SOP

## 目的与固定参数

把 YOLO11n 车辆检测 best checkpoint 导出为可由 ONNX Runtime 和 Atlas ATC 使用的模型。标准输入为 batch 1、3 通道、640×640，opset 17，固定空间尺寸和 batch，不启用 dynamic shape。

## 前置条件

先完成 model_train SOP 的训练/验证，或明确选择已归档的历史 checkpoint。导出输入路径为 model_train/train_result/yolo11n_vehicle_detection_best.pt。继续前核对权重 SHA-256 和类别顺序；历史训练报告不能冒充本次结果。

```bash
conda activate yunshao
cd project/preparation/model_vehicle_detection
```

## 导出命令

```bash
python model_export/export_onnx.py --datasets vehicle_detection --checkpoint best --imgsz 640 --device cpu --opset 17
```

脚本若在固定 train_result 路径找不到 checkpoint，会尝试 model_train/runs/train/yolo11n_vehicle_detection/weights/best.pt。为便于追溯，推荐按 model_train SOP 将本次 checkpoint 归档到固定文件名再导出。

## 输出与验收记录

预期产物为 model_export/artifacts/yolo11n_vehicle_detection_best.onnx 和 export_manifest.json。Manifest 含 Ultralytics 版本、导出参数、checkpoint 哈希和 ONNX 哈希。

记录代码提交号、完整导出命令、checkpoint/ONNX SHA-256、输入形状和工具版本。将此 ONNX 与转换脚本、AIPP 配置一并交给 model_convert；是否能被 Atlas 执行及数值是否满足阈值，由 model_val 另行确认。
