# 课堂行为检测：ONNX 导出阶段 SOP

## 目的与约定

本阶段将经过 test split 验证的 YOLO11n classroom checkpoint 导出为静态 ONNX，供 ONNX 基线评估和 Atlas ATC 转换。约定输入为 batch 1、3 通道、640×640；opset 17；默认不启用 dynamic shape。

## 输入检查

先完成 model_train 阶段，并确认固定 best checkpoint 存在：model_train/train_result/yolo11n_classroom_behavior_best.pt。记录其 SHA-256；同时检查数据 YAML 中的类别顺序为 hand-raising、reading、writing。

从模型目录根运行：

```bash
conda activate yunshao
cd project/preparation/model_classroom_behavior
```

## 导出步骤

```bash
python model_export/export_onnx.py --model-name yolo11n --datasets classroom_behavior --checkpoint best --imgsz 640 --device cpu --opset 17
```

如需复现另一个训练 checkpoint，先将它固定到 model_train/train_result/yolo11n_classroom_behavior_best.pt，或用脚本的 weights-root 参数指向对应目录。不要无记录地覆盖已归档模型。

## 输出与验收记录

输出文件为 model_export/artifacts/yolo11n_classroom_behavior_best.onnx 和 model_export/artifacts/export_manifest.json。清单记录导出时间、Ultralytics 版本、输入尺寸、opset、权重 SHA-256 和 ONNX SHA-256。

完成后记录 Git 提交、命令、Python/torch/ultralytics/onnx 版本、checkpoint 哈希、ONNX 哈希和输入形状。模型结构或推理数值检查由 model_val 阶段执行；ONNX 文件存在本身不构成精度验收。若要导出动态输入，需另外记录，并重新验证 ATC/AIPP 兼容性。
