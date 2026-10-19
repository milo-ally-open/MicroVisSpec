# 课堂模型部署产物

此目录保存模型转换/验证阶段生成的 Ascend 310B4 OM 与类别顺序，不再包含独立部署程序。正式边缘运行时、AscendCL 插件、媒体 pipeline 和 systemd 服务位于 [`project/src/device_sides/atlas200idka2/`](../../../src/device_sides/atlas200idka2/README.md)。

模型由 `model_convert/convert_atc.sh` 转换；入库清单 `model_classroom_behavior_detection.manifest.json` 将 OM 哈希、SoC、输入尺寸及类别顺序绑定到产品配置中的 `model_classroom_behavior_detection` profile。产品插件为 `libmodel_classroom_behavior_detection.so`，实现类为 `ModelClassroomBehaviorDetection`；算法语义键仍为 `classroom_behavior_detection`。安装与构建步骤见边缘运行时 README。
