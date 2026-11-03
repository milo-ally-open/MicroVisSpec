# 井盖检测研发工作流
本模型遵循[单模型标准研发流程](../README.md)，目录按 01 到 04 四阶段组织。

本模型按统一的四阶段研发目录组织。每一步的命令、环境、输入、产物和验收记录以对应阶段 README 为准。
- 训练与数据准备：[01_model_train/README.md](01_model_train/README.md)
- ONNX 导出与 Atlas ATC 转换：[02_model_convert/README.md](02_model_convert/README.md)（导出细节见 [EXPORT.md](02_model_convert/EXPORT.md)）
- ONNX/OM 精度、性能和内存评估：[03_model_val/README.md](03_model_val/README.md)
- Atlas H.264 最小视频 Demo：[04_model_demo/README.md](04_model_demo/README.md)

井盖类别顺序以固定数据集版本的 data.yaml 为准。正式产品运行时位于 project/src/device_sides/atlas200idka2/。

移植适配：训练脚本显式接受 --run-name，训练运行名和固定权重文件名均由该参数派生；默认值为 yolo11n_manhole_cover。
