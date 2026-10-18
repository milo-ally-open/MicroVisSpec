# 历史 grouped YOLO11m 评估记录

这是此前实验的历史结果，不属于当前简化的 YOLO11n 复现流程。

- Checkpoint：`yolo11m_classroom4aux_headonly_v1_classroom_behavior_best.pt`
- SHA-256：`f528401493ab6d9ff93acb95d79fa3b7b2d8e0f4ed3a774874a8a8d87d6f1149`
- Grouped validation：711 张，768 输入、TTA、NMS IoU 0.50；mAP50 / mAP50-95 为 **0.740695 / 0.553861**。
- SCB5 开发诊断：193 张，960 输入、TTA、NMS IoU 0.50；mAP50 / mAP50-95 为 **0.660661 / 0.486202**。该数据参与过模型开发，不是盲测。

训练数据和权重未随代码提交，无法仅凭仓库从头复现此 checkpoint。历史 ONNX 导出参数及文件哈希见 [`model_export/classroom4aux_headonly_768_export_manifest.json`](../../model_export/classroom4aux_headonly_768_export_manifest.json)。历史评估不能代替当前 YOLO11n 工作流的训练与测试结果；当前流程见 [`WORKFLOW.md`](../../WORKFLOW.md)。
