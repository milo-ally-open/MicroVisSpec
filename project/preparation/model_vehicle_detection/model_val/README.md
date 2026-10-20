# 车辆检测：模型评估阶段 SOP

此阶段有四个独立程序：accuracy_eval 计算总体/逐类 P、R、F1、AP50、mAP50-95；performance_eval 记录流水线延迟和 FPS；memory_eval 记录模型文件、RSS、张量和 NPU 快照；precision_eval 验证 ONNX 与 OM 是否来自相同评测输入并汇总差异。

## 数据、环境和边界

工作站 ONNX 在 yunshao 环境中运行，Atlas OM 在板载 CANN/ais_bench Python 环境中运行。两端必须使用 Roboflow vehicle-detection-tqbaf-7cck5 v1 的同一完整 test split、data.yaml、类别顺序、640 尺寸和阈值。数据集不随仓库保证提供；必须先还原并核对图片/标签。历史报告曾记录某些标签被忽略，复现时须以本次实际评估数量为准。

工作站 ONNX Runtime 1.20.1 固定使用 CPUExecutionProvider，--device 参数只用于 OM 的 ais_bench InferSession。Atlas 基线为 CANN 7.0.RC1、Miniconda Python 3.9.2、NumPy 1.23.5 和板载 ais_bench/aclruntime。

```bash
conda activate yunshao
cd project/preparation/model_vehicle_detection
```

## 精度评估

工作站 ONNX：

```bash
python model_val/accuracy_eval.py --backend onnx --model model_export/artifacts/yolo11n_vehicle_detection_best.onnx --data model_train/datasets/vehicle_detection/data.yaml --split test --output model_val/reports/release/onnx --imgsz 640 --conf 0.001 --iou 0.45 --device 0 --warmup 5
```

板端 OM：

在 Atlas 模型目录内准备与工作站相同的 data.yaml、图像/标签目录、model_val 脚本和部署 OM，并保持 YAML/数据内容一致。

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
/usr/local/miniconda3/bin/python model_val/accuracy_eval.py --backend om --model model_deployment/yolo11n_vehicle_detection_best.om --data model_train/datasets/vehicle_detection/data.yaml --split test --output model_val/reports/release/om --imgsz 640 --conf 0.001 --iou 0.45 --device 0 --warmup 5
```

每份输出含 backend_report.json 与 backend_per_class.csv。正式对比不加 --limit；限制图片数量仅用于排查运行错误。

## 性能和内存

ONNX 端示例：

```bash
python model_val/performance_eval.py --backend onnx --model model_export/artifacts/yolo11n_vehicle_detection_best.onnx --data model_train/datasets/vehicle_detection/data.yaml --split test --output model_val/reports/release/onnx_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
python model_val/memory_eval.py --backend onnx --model model_export/artifacts/yolo11n_vehicle_detection_best.onnx --data model_train/datasets/vehicle_detection/data.yaml --split test --output model_val/reports/release/onnx_memory.json --imgsz 640 --device 0 --limit 1
```

在 Atlas 执行 OM 性能和内存测量：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
/usr/local/miniconda3/bin/python model_val/performance_eval.py --backend om --model model_deployment/yolo11n_vehicle_detection_best.om --data model_train/datasets/vehicle_detection/data.yaml --split test --output model_val/reports/release/om_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
/usr/local/miniconda3/bin/python model_val/memory_eval.py --backend om --model model_deployment/yolo11n_vehicle_detection_best.om --data model_train/datasets/vehicle_detection/data.yaml --split test --output model_val/reports/release/om_memory.json --imgsz 640 --device 0 --limit 1
```

memory_eval 附带 npu-smi 快照；进程 RSS 不等于 NPU 显存。性能记录应同时记录预热、设备和系统负载。

## ONNX/OM 对比和归档

将 OM 报告复制回工作站后运行：

```bash
python model_val/precision_eval.py --onnx-report model_val/reports/release/onnx/onnx_report.json --om-report model_val/reports/release/om/om_report.json --output model_val/reports/release/onnx_vs_om --max-map-drop 0.02 --max-class-ap50-drop 0.05
```

对比器会校验 dataset YAML、图片和标签清单哈希、split、图片数、尺寸、阈值及类别，并生成 JSON/CSV 差异。默认阈值为总体 mAP50-95 下降不超过 0.02、任一类别 AP50 下降不超过 0.05。保留代码提交、数据/模型哈希、工具版本、完整命令和原始报告。没有完整且同源的 test split 报告时，不形成精度等价结论。
