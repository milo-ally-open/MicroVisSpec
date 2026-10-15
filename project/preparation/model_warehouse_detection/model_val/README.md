# 仓储物品检测：分项评估

model_val 中只提供四个独立评估脚本，每个脚本的输入均使用具名 --参数名 值：

- ccuracy_eval.py：固定数据集上的总体与逐类别 Precision、Recall、F1、AP50、mAP50-95；报告数据集和标签清单哈希。
- performance_eval.py：预处理、推理、后处理延迟的 mean、p50、p95、p99 和流水线 FPS。
- memory_eval.py：模型文件大小、进程峰值 RSS、输入/输出张量内存；板端可附带
pu-smi info 快照。
- precision_eval.py：校验同一测试集的 ONNX 与 OM 精度报告并生成总体及逐类别差异和验收结论。

ONNX 使用 WSL yunshao 环境的 ONNX Runtime；OM 在 Atlas 200I DK A2 上使用板载 Miniconda ase 中的 clruntime。板端先执行 source /usr/local/Ascend/ascend-toolkit/set_env.sh。不要用系统 Python。完整测试集当前不在仓库中；恢复固定 test split 后才能运行并声称精度结果。示例的 --limit 20 仅用于快速冒烟，不能替代完整评估。

## 精度分析

从此模型目录运行 ONNX 基线：

`ash
python model_val/accuracy_eval.py --backend onnx --model model_export/artifacts/yolo11n_warehouse_detection_best.onnx --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/test --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
`

板端将同一份数据集、data.yaml、model_val 和 OM 放到包目录中，然后运行：

`ash
python model_val/accuracy_eval.py --backend om --model model_deployment/yolo11n_warehouse_detection_best.om --data /root/model_library/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/test --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
`

输出 onnx_report.json / om_report.json 和各自的 *_per_class.csv。报告包含总体 micro Precision/Recall/F1、macro AP50、mAP50-95 及每个类别的指标。AP 使用 IoU 0.50 至 0.95、间隔 0.05 的 101 点插值。

## 性能评估

`ash
python model_val/performance_eval.py --backend onnx --model model_export/artifacts/yolo11n_warehouse_detection_best.onnx --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/test/onnx_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
`

OM 测量时将 --backend onnx 与模型路径替换为 --backend om --model model_deployment/yolo11n_warehouse_detection_best.om，并在板端执行。--limit 20 可以限制快速试跑图片数。

## 内存评估

`ash
python model_val/memory_eval.py --backend onnx --model model_export/artifacts/yolo11n_warehouse_detection_best.onnx --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/test/onnx_memory.json --imgsz 640 --device 0 --limit 1
`

板端 OM 示例：

`ash
python model_val/memory_eval.py --backend om --model model_deployment/yolo11n_warehouse_detection_best.om --data /root/model_library/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/test/om_memory.json --imgsz 640 --device 0 --limit 1
`

其中进程 RSS 是进程峰值常驻内存，不等同于 NPU 显存；NPU 快照另存为同名 .npu-smi.txt 文件。

## 精度验证与对比

把板端 om_report.json 复制到开发机的 model_val/reports/test/ 后运行：

`ash
python model_val/precision_eval.py --onnx-report model_val/reports/test/onnx_report.json --om-report model_val/reports/test/om_report.json --output model_val/reports/test/onnx_vs_om --max-map-drop 0.02 --max-class-ap50-drop 0.05
`

脚本检查 YAML 哈希、图片与标注清单哈希、split、图片数、尺寸、阈值和类别一致性；生成总体/逐类别 JSON 与 CSV。默认判定阈值为总体 mAP50-95 下降不超过 0.02、任一类别 AP50 下降不超过 0.05。没有同一固定 test split 的两份报告时拒绝进行对比。
