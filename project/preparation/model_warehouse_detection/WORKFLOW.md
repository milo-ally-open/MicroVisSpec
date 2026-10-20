# 仓储检测阶段索引

本文件只用于定位各阶段 SOP。复现命令、输入、产物与验收记录以阶段目录内的 README.md 为准；模型根目录不设置总 README。

- 数据下载、分析、训练和 PyTorch 验证：[model_train/README.md](model_train/README.md)
- 固定输入 ONNX 导出：[model_export/README.md](model_export/README.md)
- Atlas 310B4 ATC 转换：[model_convert/README.md](model_convert/README.md)
- ONNX/OM 精度、性能、内存评估：[model_val/README.md](model_val/README.md)
- Atlas H.264 部署验收 demo：[model_deployment/README.md](model_deployment/README.md)

固定数据来源、硬件约束、环境和完整追溯要求见对应阶段 SOP；已有历史指标不是本次重新评估结果。
