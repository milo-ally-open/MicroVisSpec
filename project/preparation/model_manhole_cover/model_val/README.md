# 井盖检测：模型评估阶段 SOP

本阶段分别测量 ONNX（开发机）和 OM（Atlas）的检测精度、延迟/FPS、进程 RSS/张量大小，并用 precision_eval 检查两后端差异。视频 smoke 只检查推理/媒体链路，不能代替本阶段的 test split 指标。

## 固定比较条件

两端必须使用同一个 Roboflow 数据快照、同一 data.yaml、完整 test split、类别顺序、640 输入和相同阈值（conf 0.25、iou 0.45）。数据路径和图像/标签清单哈希必须一致。若缺标签、图片无法读取、标注类型不兼容或实际参与数量不同，先解决或明确记录，不要声称完整集结果。脚本的默认 test split 为 test；报告 JSON 记录 metric_implementation 字段（两端须一致，避免跨机器不同实现对比）。完整精度评估不设置 --limit。

工作站使用 yunshao 环境；Atlas 使用板载 CANN 与提供 ais_bench 的 Python 环境：

ONNX Runtime 1.20.1 固定使用 CPUExecutionProvider；脚本的 --device 参数只用于 OM 的 ais_bench InferSession。Atlas 基线使用 CANN 7.0.RC1、Miniconda Python 3.9.2、NumPy 1.23.5，以及板载 ais_bench/aclruntime。

```bash
conda activate yunshao
cd project/preparation/model_manhole_cover
```

## 精度报告

开发机 ONNX：

```bash
python model_val/accuracy_eval.py --backend onnx --model model_export/artifacts/yolo11n_manhole_cover_best.onnx --data model_train/datasets/manhole_cover/data.yaml --split test --output model_val/reports/release/onnx --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
```

把同一 data.yaml、完整数据快照、model_val 脚本及 OM 放到 Atlas 的模型目录，保持相对路径和文件内容一致；在模型目录运行：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
/usr/local/miniconda3/bin/python model_val/accuracy_eval.py --backend om --model model_deployment/yolo11n_manhole_cover_best.om --data model_train/datasets/manhole_cover/data.yaml --split test --output model_val/reports/release/om --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
```

输出分别包含 onnx_report.json / om_report.json 和逐类 CSV。完整精度对比不得设置 limit；快速试跑可加 --limit，但必须标记为 smoke。

## 性能与内存

分别在 ONNX 工作站和 OM 板端运行。ONNX 示例：

```bash
python model_val/performance_eval.py --backend onnx --model model_export/artifacts/yolo11n_manhole_cover_best.onnx --data model_train/datasets/manhole_cover/data.yaml --split test --output model_val/reports/release/onnx_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
python model_val/memory_eval.py --backend onnx --model model_export/artifacts/yolo11n_manhole_cover_best.onnx --data model_train/datasets/manhole_cover/data.yaml --split test --output model_val/reports/release/onnx_memory.json --imgsz 640 --device 0 --limit 1
```

性能 JSON 报告预处理、推理、后处理分位耗时和 FPS；内存报告给出模型文件、RSS 与张量大小，板端另写同名 npu-smi 快照。RSS 不是 NPU 显存；记录板端温度/频率和并发负载，避免跨不同运行条件直接比较。

Atlas OM 性能/内存命令：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
/usr/local/miniconda3/bin/python model_val/performance_eval.py --backend om --model model_deployment/yolo11n_manhole_cover_best.om --data model_train/datasets/manhole_cover/data.yaml --split test --output model_val/reports/release/om_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
/usr/local/miniconda3/bin/python model_val/memory_eval.py --backend om --model model_deployment/yolo11n_manhole_cover_best.om --data model_train/datasets/manhole_cover/data.yaml --split test --output model_val/reports/release/om_memory.json --imgsz 640 --device 0 --limit 1
```

## ONNX/OM 差异判断

把 OM 报告复制到工作站的相同 reports/release 目录，再执行：

```bash
python model_val/precision_eval.py --onnx-report model_val/reports/release/onnx/onnx_report.json --om-report model_val/reports/release/om/om_report.json --output model_val/reports/release/onnx_vs_om --max-map-drop 0.02 --max-class-ap50-drop 0.05
```

脚本核对 YAML、测试图片/标签哈希、split、尺寸、阈值和类别，并产出总体与逐类 JSON/CSV。归档代码版本、数据哈希、模型 SHA-256、命令、机器/软件版本和原始报告；不把小样本 smoke 的数值写成正式精度。