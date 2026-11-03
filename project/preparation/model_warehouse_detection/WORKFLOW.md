# 仓储检测研发工作流
本模型遵循[单模型标准研发流程](../README.md)，目录按 01 到 04 四阶段组织。

本模型按统一的四阶段研发目录组织。复现命令、输入、产物与验收记录以各阶段 README 为准。
- 数据下载、分析、训练和 PyTorch 验证：[01_model_train/README.md](01_model_train/README.md)
- ONNX 导出与 Atlas ATC 转换：[02_model_convert/README.md](02_model_convert/README.md)（导出细节见 [EXPORT.md](02_model_convert/EXPORT.md)）
- ONNX/OM 精度、性能和内存评估：[03_model_val/README.md](03_model_val/README.md)
- Atlas H.264 最小视频 Demo：[04_model_demo/README.md](04_model_demo/README.md)

固定数据来源、硬件约束、环境和完整追溯要求见对应阶段 SOP；历史指标与本次实测分开记录。
