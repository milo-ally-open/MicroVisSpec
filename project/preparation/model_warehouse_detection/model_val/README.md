# 仓储检测：模型评估阶段 SOP

本阶段分别评估 ONNX 和 Atlas OM 的检测精度、推理流水线性能和内存占用，并校验后端精度差异。四个脚本分别为 accuracy_eval.py、performance_eval.py、memory_eval.py、precision_eval.py；视频 demo 的 smoke 不替代测试集精度。

## 固定数据和环境

固定使用 Roboflow warehouse-detection-xtbsn v1 的同一完整 test split。工作站运行 ONNX，Atlas 200I DK A2 运行 OM。确保两端 data.yaml、图片和 labels 完全相同，类别名/顺序一致，输入尺寸 640，阈值一致。仓储历史测试曾因检测框与分割标签混用跳过图片；复现必须检查脚本报告中的参与/跳过数量。

工作站 ONNX Runtime 1.20.1 固定使用 CPUExecutionProvider，--device 参数只用于 OM 的 ais_bench InferSession。Atlas 基线为 CANN 7.0.RC1、Miniconda Python 3.9.2、NumPy 1.23.5 和板载 ais_bench/aclruntime。

```bash
conda activate yunshao
cd project/preparation/model_warehouse_detection
```

## 精度评估

开发机 ONNX：

```bash
python model_val/accuracy_eval.py --backend onnx --model model_export/artifacts/yolo11n_warehouse_detection_best.onnx --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/release/onnx --imgsz 640 --conf 0.001 --iou 0.45 --device 0 --warmup 5
```

Atlas OM：

在 Atlas 模型目录内准备与工作站相同的 data.yaml、图像/标签目录、model_val 脚本和部署 OM，并保持 YAML/数据内容一致。

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
/usr/local/miniconda3/bin/python model_val/accuracy_eval.py --backend om --model model_deployment/yolo11n_warehouse_detection_best.om --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/release/om --imgsz 640 --conf 0.001 --iou 0.45 --device 0 --warmup 5
```

两份结果各自产生 backend_report.json 和逐类 CSV。完整精度评估不得添加 --limit；小样本试跑必须标记为 smoke。

## 性能和内存评估

ONNX 示例：

```bash
python model_val/performance_eval.py --backend onnx --model model_export/artifacts/yolo11n_warehouse_detection_best.onnx --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/release/onnx_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
python model_val/memory_eval.py --backend onnx --model model_export/artifacts/yolo11n_warehouse_detection_best.onnx --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/release/onnx_memory.json --imgsz 640 --device 0 --limit 1
```

Atlas OM 性能和内存命令：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
/usr/local/miniconda3/bin/python model_val/performance_eval.py --backend om --model model_deployment/yolo11n_warehouse_detection_best.om --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/release/om_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
/usr/local/miniconda3/bin/python model_val/memory_eval.py --backend om --model model_deployment/yolo11n_warehouse_detection_best.om --data model_train/datasets/warehouse_detection/data.yaml --split test --output model_val/reports/release/om_memory.json --imgsz 640 --device 0 --limit 1
```

内存报告记录 RSS 与 tensor bytes，并写入 npu-smi 快照；RSS 不等于 NPU 显存。性能结果必须注明设备、CANN、Python、预热次数与系统负载。

## 差异分析与结果归档

将 OM 报告复制回工作站后运行：

```bash
python model_val/precision_eval.py --onnx-report model_val/reports/release/onnx/onnx_report.json --om-report model_val/reports/release/om/om_report.json --output model_val/reports/release/onnx_vs_om --max-map-drop 0.02 --max-class-ap50-drop 0.05
```

脚本对比 data.yaml、样本/标签清单哈希、split、样本数、尺寸、阈值和类别，输出总体/逐类差异。默认通过线为 mAP50-95 下降不超过 0.02、任一类别 AP50 下降不超过 0.05。归档代码提交、数据和模型 SHA-256、工具版本、设备信息及未过滤的原始报告；不基于不完整数据写精度结论。
