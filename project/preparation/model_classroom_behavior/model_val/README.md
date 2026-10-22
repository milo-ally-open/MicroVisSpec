# 课堂行为检测：分项评估

四个评估脚本与 `model_vehicle_detection/model_val/` 同源，均使用具名参数：

- `accuracy_eval.py`：固定数据集上的总体与逐类别 Precision、Recall、F1、AP50、mAP50-95；报告数据和标签清单哈希。
- `performance_eval.py`：预处理、推理、后处理延迟分位数和流水线 FPS。
- `memory_eval.py`：模型文件大小、峰值 RSS 与张量内存；板端可附加 `npu-smi info`。
- `precision_eval.py`：比较同一测试集上的 ONNX 与 OM 报告。

## ONNX 精度

```bash
python model_val/accuracy_eval.py --backend onnx --model model_export/artifacts/yolo11n_classroom_behavior_best.onnx --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/test --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
```

## Atlas OM 精度

把固定版本数据集、`model_val/` 和 OM 模型放到板端后，执行：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
python model_val/accuracy_eval.py --backend om --model model_deployment/yolo11n_classroom_behavior_best.om --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/test --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
```

## 性能与内存

将上面的 `--backend onnx --model ...onnx` 分别用于开发机测试，`--backend om --model ...om` 用于板端实测。性能脚本输出到 `model_val/reports/test/onnx_performance.json` 或 `om_performance.json`；内存脚本输出到对应的 `onnx_memory.json` 或 `om_memory.json`。

## ONNX/OM 精度对比

```bash
python model_val/precision_eval.py --onnx-report model_val/reports/test/onnx_report.json --om-report model_val/reports/test/om_report.json --output model_val/reports/test/onnx_vs_om --max-map-drop 0.02 --max-class-ap50-drop 0.05
```

没有同一固定 test split 的两份完整报告时，不作精度对比结论。烟测和少量样本试跑不能替代完整测试集评估。
