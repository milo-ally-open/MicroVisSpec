# 分组划分课堂行为模型

## 当前推荐权重

`yolo11m_classroom4aux_headonly_v1_classroom_behavior_best.pt` 是当前推荐的精度优先 checkpoint。权重 SHA-256 为 `f528401493ab6d9ff93acb95d79fa3b7b2d8e0f4ed3a774874a8a8d87d6f1149`。训练参数见同目录的 `yolo11m_classroom4aux_headonly_v1_classroom_behavior_training.json`。

在 711 张 grouped validation 图像上，768 输入、TTA、NMS IoU 0.50 得到 mAP50 / mAP50-95 **0.740695 / 0.553861**。在 193 张 SCB5 开发诊断图像上，960 输入、TTA、NMS IoU 0.50 得到 **0.660661 / 0.486202**；SCB5 曾用于开发，不是盲测。目标课堂视频尚未提供，因此目标域表现未知。

固定 768 输入 ONNX 已导出。导出参数、checkpoint 与 ONNX 哈希见 [`model_export/classroom4aux_headonly_768_export_manifest.json`](../../model_export/classroom4aux_headonly_768_export_manifest.json)。ONNX 未包含 TTA，不能直接用 PyTorch 的 TTA 指标代表其部署精度。

## 其他实验

SCB5 微调、全层微调、不同分辨率及补标数据实验均保留在本机训练目录用于研发复核，但不是当前推荐权重。实验权重、完整训练运行和数据集不纳入 Git；需要发布模型文件时，使用 Git LFS 或 Release 单独管理。

当前训练记录的初始化权重是较早的 `yolo11m_scb5aux_v2` checkpoint，训练数据也位于仓库外；二者没有包含在普通 Git 提交中。因此仓库记录了当前模型的参数、哈希和评估证据，但克隆仓库本身不足以从头复现这次微调。若要提供完整复现包，需先确认数据与基础权重可再分发，再单独发布对应文件。

完整过程与限制见 [`RESULTS.md`](../../RESULTS.md)。Atlas 200I DK A2 的 OM 转换、板端精度/性能/内存测量和视频演示尚未完成。

第三方来源的一次性审计脚本依赖仓库外数据目录，当前保留在本机且由 `.gitignore` 排除；审计结论和来源许可摘要记录在 `RESULTS.md`。仓库保留可移植的下载、分析、训练、验证和目标课堂数据准备入口。
