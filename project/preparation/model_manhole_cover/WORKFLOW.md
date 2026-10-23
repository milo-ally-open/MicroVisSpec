# 井盖检测阶段索引

本文件只用于定位各阶段 SOP。复现步骤以各阶段目录内的 README.md 为准；模型研发目录根部不设置总 README。

- 下载、数据分析、训练和 PyTorch 验证：[model_train/README.md](model_train/README.md)
- 固定输入 ONNX 导出：[model_export/README.md](model_export/README.md)
- Atlas 310B4 ATC 转换：[model_convert/README.md](model_convert/README.md)
- ONNX/OM 精度、性能、内存评估：[model_val/README.md](model_val/README.md)
- Atlas H.264 部署验收 demo：[model_deployment/README.md](model_deployment/README.md)

数据源、环境、命令、产物和追溯要求分别记录在对应阶段 SOP 中。井盖模型类别顺序以选定 Roboflow 数据集版本的 `data.yaml` names 为准（下载脚本固化并校验）；正式产品 runtime 位于 project/src/device_sides/atlas200idka2/。

移植适配说明：本场景由 model_classroom_behavior 五阶段结构移植而来。`3-train-model.py` 相对存量 SOP 增加了显式 `--run-name` 参数（训练运行名与 train_result 产物名均由其派生），以满足唯一 run-name 与失败重试隔离要求；不带该参数时行为与存量骨架一致（默认 `yolo11n_manhole_cover`）。