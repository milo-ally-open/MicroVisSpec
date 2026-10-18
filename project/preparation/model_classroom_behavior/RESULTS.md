# 课堂行为检测模型开发记录与结果

## 当前提交基线（2026-10-18）

- 推荐权重：`model_train/train_result_grouped/yolo11m_classroom4aux_headonly_v1_classroom_behavior_best.pt`，SHA-256：`f528401493ab6d9ff93acb95d79fa3b7b2d8e0f4ed3a774874a8a8d87d6f1149`。
- grouped validation：711 张图，768 输入、TTA、NMS IoU 0.50，mAP50 / mAP50-95 为 **0.740695 / 0.553861**。
- SCB5 开发诊断：193 张图，960 输入、TTA、NMS IoU 0.50，mAP50 / mAP50-95 为 **0.660661 / 0.486202**。该数据参与过模型开发，不是盲测。
- 新课堂目标域尚无视频，实际课堂表现仍未验证。Atlas 200I DK A2 的 OM 转换、板端精度/性能/内存测量和视频演示也尚未完成。
- 训练、验证运行目录和数据集不属于 Git 提交内容；只保留脚本、配置、结果摘要及经筛选的模型发布产物。

## 任务与数据

- 模型：YOLO11n 目标检测，类别顺序为 `hand-raising`、`reading`、`writing`。
- 数据：Roboflow Universe `main-vdoys/student-dataset-demo` v1，页面标记 Public Domain。
- 划分：train 3,320 张、valid 942 张、test 477 张；全部图片可解码，标签文件齐全。标注文件用多边形表示，Ultralytics 训练检测模型时按外接框处理。
- 标注框：train 16,784、valid 4,616、test 2,609。各类总量不均衡，`writing` 明显较少。
- 原始随机划分虽然没有 SHA-256 完全相同的图片，但后续抽查发现 train/valid/test 之间存在同一课堂片段的相邻或近重复帧。因此原首轮 test mAP50=0.6636 只保留作历史基线，可能偏乐观，不能与下面的近重复分组结果直接比较。

数据分析表、样例和问题清单位于 `model_train/dataset_analysis/classroom_behavior/`。

## 训练设置

- 环境：Windows Python 3.12.10、Ultralytics 8.4.118、PyTorch 2.5.1+cu124、RTX 4060 Laptop GPU。
- 初始化：Ultralytics 官方 `yolo11n.pt` 预训练权重。
- 参数：100 epochs 上限、640 输入尺寸、batch 8、workers 4、seed 42、deterministic、patience 30。实际完成 100 轮。
- 验证集最佳轮次：99；valid mAP50 0.6775、mAP50-95 0.4845。
- 训练曲线和验证图表：`model_train/runs/train/yolo11n_classroom_behavior/`。

## 留出测试集结果（PyTorch best checkpoint）

| 类别 | P | R | F1 | AP50 | AP50-95 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 全部 | 0.6135 | 0.6637 | 0.6376 | 0.6636 | 0.4752 |
| 举手 | 0.7102 | 0.7129 | 0.7115 | 0.7586 | 0.5268 |
| 阅读 | 0.6314 | 0.6927 | 0.6606 | 0.6934 | 0.4968 |
| 书写 | 0.4988 | 0.5857 | 0.5388 | 0.5389 | 0.4020 |

共评估 477 张 test 图像、2,609 个标注目标。RTX 4060 上报告的推理时间约 3.2 ms/图（不含视频解码和业务逻辑）。完整报告、预测 JSON、混淆矩阵和样例保存在 `model_train/runs/validation/`。

## ONNX 转换

- 固定输入：`1×3×640×640`，opset 17。
- 文件：`model_export/artifacts/yolo11n_classroom_behavior_best.onnx`，约 10.1 MB。
- best PT SHA-256：`837c98bc0451e77168a922ba7efdbd2dea7cbd78d9c2ec21144402b0d77f7574`
- ONNX SHA-256：`f216cb2976d31565fd75da6aa6b9f6d6563786504dd66c04a71a855f7cf710f7`
- 同一 test split 经 Ultralytics ONNX Runtime CPUExecutionProvider 评估：P 0.6153、R 0.6566、mAP50 0.6505、mAP50-95 0.4689。相对 PyTorch 差值为 -0.0131 / -0.0063 mAP。
- 类别 AP50：举手 0.751、阅读 0.666、书写 0.534。
- Ryzen 9 7940HX CPU 上端到端平均约 31.6 ms/图（31.6 FPS），ONNX Runtime 平均推理约 25.1 ms/图；进程峰值内存约 121 MiB。该数据不是 Atlas 板端结果。
- `model_val/reports/test/onnx_report.json` 是项目自带 ONNX/OM 配对评估器的结果（macro AP50 0.6234、mAP50-95 0.4505）；它与 Ultralytics 的 AP 实现不同，应使用该评估器分别对 ONNX 和 OM 做相对比较，不与上面的 Ultralytics 主指标混算。

ONNX 精度、延迟、内存报告位于 `model_val/reports/test/`；Ultralytics 可视化结果位于 `model_val/runs_onnx/onnx_test_iou070/`。

## 结论与下一步

### 第二轮：近重复分组的 YOLO11s

- 为避免相邻或近重复画面跨集合，依据 64 位 dHash 汉明距离 ≤4 建图并取连通组，同组图片放在同一 split。4,739 张原始图片组成 4,540 组，其中 109 组含多张图片；分组脚本与清单见 `model_train/5-make-grouped-split.py` 和 `model_train/datasets_grouped/classroom_behavior/`。
- 划分：train 3,317 张、valid 711 张、test 711 张；训练、验证、测试之间没有 dHash 近重复组交叉。这个方法能阻断查到的近重复帧，但数据没有源视频 ID，因此不能保证同一源视频中所有外观差异较大的帧都在同一组。
- 初始化：官方 `yolo11s.pt` 预训练权重；AdamW，640 输入，batch 8，150 轮，学习率 0.001，seed 42。训练运行目录：`model_train/runs_grouped/train/yolo11s_grouped_classroom_behavior/`。
- 训练过程日志记录的最佳轮次为第 95 轮：P 0.6565、R 0.6831、mAP50 0.7133、mAP50-95 0.5255。后续用项目独立评估脚本重评该固定 checkpoint（640 输入、grouped validation）得到 mAP50=0.7095、mAP50-95=0.5287；后续候选对照统一采用此独立复核值。

一次性保留测试集评估（711 张图，3,213 个目标）：

| 类别 | P | R | F1 | AP50 | AP50-95 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 全部 | 0.6453 | 0.6888 | 0.6663 | **0.6959** | 0.5075 |
| 举手 | 0.7610 | 0.7414 | 0.7511 | 0.7861 | 0.5611 |
| 阅读 | 0.5944 | 0.7041 | 0.6446 | 0.6706 | 0.4924 |
| 书写 | 0.5805 | 0.6209 | 0.6000 | 0.6309 | 0.4689 |

验证集超过 0.7，留出测试集为 0.6959，接近但未达到 0.7。测试集表现较弱的是阅读和书写；下一轮应优先检查这两类标注质量、增加不同教室与视角的样本，并且只用新的验证集做调参。当前测试集已经用于本轮最终评估，不应用它反复挑选后续模型。

- best checkpoint：`model_train/train_result_grouped/yolo11s_grouped_classroom_behavior_best.pt`（19.2 MB）；训练记录与 SHA-256 在同目录 JSON。
- 测试报告、预测 JSON、混淆矩阵和可视化样例：`model_train/runs_grouped/validation/`。
- RTX 4060 测试推理约 7.0 ms/图（不含视频解码和业务逻辑）。

首轮模型及其 ONNX 结果仍可作为历史演示基线；若要发布第二轮的 ONNX/OM，请从上述 best checkpoint 单独导出，并在新的独立数据上做部署后评估。

公开数据包含学生图像。若转向真实课堂部署，先获得学校/监护人授权，限制访问与保留期限，并仅做匿名统计；模型输出不应用于学生身份识别、成绩评定或纪律处分。

Atlas 200I DK A2 的 ATC/OM 转换、精度/性能/内存测量和视频 demo 尚未执行，需要在真实开发板上完成后才能声称端侧部署通过。

## 后续提分诊断（2026-10-18）

- 只分析近重复分组后的 validation（711 张）；没有再次读取已评估过的 test split。
- 用 conf=0.25、IoU=0.5 对齐预测与标注，生成错误清单和可视化：`model_train/runs_grouped/validation/val_error_audit/`。这些固定阈值的 FP/FN 是诊断统计，不能替代 AP/mAP。
- 最集中的错误是 `reading` 与 `writing` 互相混淆：未匹配预测中有 178 个把 `writing` 标成 `reading`，166 个把 `reading` 标成 `writing`。`writing` 在这组验证图像中只有 449 个标注目标，`reading` 有 1,529 个；writing 的 AP50 为 0.649，reading 为 0.697。
- 对同一 best checkpoint 做 768 输入尺寸的 validation 探针：mAP50=0.7058、mAP50-95=0.5190；640 输入基线为 0.7095、0.5287。分辨率增大没有收益，writing AP50 从 0.649 降至 0.644，因此暂不以提高 imgsz 作为提分方案。结果位于 `model_train/runs_grouped/validation/resolution_probe/classroom_behavior_val_20261018_001159/`。
- 建议后续优先人工抽查读/写混淆图片及其标注，先统一可操作的类别判定规则，再按规则修正标签或补充不同教室、视角、清晰度的写字样本；之后只用 validation 比较针对性重训。若要宣称泛化性能提升，需要另采集从未参与训练/调参、按源视频隔离的新测试数据，因为当前 test 已在上一轮评估过。
- 本地数据说明和 Roboflow 项目页没有提供 reading/writing 的操作性标注定义；复核建议和需填写的字段见 `model_train/dataset_analysis/classroom_behavior/READ_WRITE_REVIEW_GUIDE.md`。`reading_writing_review.csv` 含 344 个跨类候选，尚未由人工确认，不能据此直接改标签。
- 类别加权微调探针（`cls_pw=0.5`，从当前 best 初始化，640 输入）运行至第 15 轮后停止。对其独立 best checkpoint 重新评估 validation：mAP50=0.6961、mAP50-95=0.5172；逐类 AP50 为举手 0.7699、阅读 0.6859、书写 0.6327，均低于原模型的 0.7827、0.6968、0.6490。候选权重留在训练探针目录中，不替换当前 best，也未评估 test。
- 又检查并下载了 Roboflow `studentdetection/student-etyh9` v5（CC BY 4.0）。按 dHash 汉明距离 ≤4 与现有全部 grouped split 对照后，从其 8,968 张 train 图中排除 4,045 张近重复图（其中对当前 validation/test 分别发现 655/692 个近重复对）；仅保留 1,930 张含目标类别的图，并映射到三类。合并训练清单共 5,247 张；validation/test 清单仍为原 711/711 张。来源、过滤方式、类别映射与归属见 `model_train/datasets_augmented/classroom_behavior/AUXILIARY_DATA.md`。
- 辅助数据微调探针（640 输入、无类别加权）到第 15 轮后停止。候选 best 在独立 grouped validation 评估为 P=0.6548、R=0.6764、mAP50=0.7054、mAP50-95=0.5202，未超过原模型 0.7095/0.5287。逐类 AP50：举手 0.7681（原 0.7827）、阅读 0.7084（原 0.6968）、书写 0.6398（原 0.6490）。阅读有所改善，但举手、书写及总体指标下降，因此候选未取代现有 best，也未评估 test。
- 写作类定向数据微调探针：在 3,317 张原训练图像之外，加入经近重复过滤后 142 张“有 writing、无 reading” 的补充图像（合计 3,459 张；补充标签共 1,016 个 writing、11 个 hand-raising），从原 grouped best 初始化，AdamW、640 输入、lr0=0.0001。训练记录在第 28 轮结束，保留 run 内 best 权重。为保证公平，用相同验证脚本分别重评原始 best 和候选 best，均为 grouped validation 711 张：

  | 指标 | 原始 best | 写作定向候选 | 差值 |
  | --- | ---: | ---: | ---: |
  | mAP50 | 0.70947 | 0.71055 | +0.00108 |
  | mAP50-95 | 0.52865 | 0.52989 | +0.00123 |
  | Precision | 0.67547 | 0.68120 | +0.00573 |
  | Recall | 0.66347 | 0.65560 | -0.00787 |

  逐类 AP50 变化：举手 0.78271→0.78474，阅读 0.69676→0.70566，书写 0.64895→0.64126。整体指标只提升约 0.001，且书写类下降、recall下降，当前证据不足以认定有稳定提升，因此继续保留原始 best 为正式模型；候选仅供后续对照。候选 checkpoint：`model_train/runs_grouped/train/yolo11s_writingaux_ft_classroom_behavior/weights/best.pt`。同一验证设置下的完整报告分别位于 `model_train/runs_grouped/validation/writingaux_final_check/classroom_behavior_val_20261018_023847/`（候选）和 `.../classroom_behavior_val_20261018_023932/`（原始 best）。未使用 test split。

## 第三轮：YOLO11m 分组训练

- 从 Ultralytics 预训练 `yolo11m.pt` 初始化，使用原 grouped train 3,317 张、validation 711 张；640 输入、batch 4、AdamW、100 轮上限、patience 20、seed 42。运行与参数记录在 `model_train/runs_grouped/train/yolo11m_grouped_classroom_behavior/` 和 `model_train/train_result_grouped/yolo11m_grouped_classroom_behavior_training.json`。
- 对发布的 YOLO11m best 与 YOLO11s 基线用相同验证脚本、相同 grouped validation、相同 batch=4 独立复评：

  | 指标 | YOLO11s 基线 | YOLO11m 候选 | 差值 |
  | --- | ---: | ---: | ---: |
  | mAP50 | 0.70949 | **0.71417** | +0.00468 |
  | mAP50-95 | 0.52868 | **0.53458** | +0.00590 |
  | Precision | 0.67564 | 0.64931 | -0.02634 |
  | Recall | 0.66369 | 0.68915 | +0.02546 |

  逐类 AP50：举手 0.78272→0.78322，阅读 0.69680→0.69413，书写 0.64895→0.66517。主要收益来自书写类，阅读 AP50 轻微下降。候选 best 是训练第 89 轮；独立验证报告位于 `model_train/runs_grouped/validation/yolo11m_final_check/classroom_behavior_val_20261018_063125/`，同设置 YOLO11s 对照位于 `.../classroom_behavior_val_20261018_063207/`。
- YOLO11m best 权重约 40.6 MB，参数约 20.0M；YOLO11s 约 19.2 MB、9.4M 参数。该验证运行在 RTX 4060 上分别约 17.1 ms/图和 6.7 ms/图，未含视频解码。故将 YOLO11m 记录为当前验证精度候选，YOLO11s 保留为小模型基线；尚未在 Atlas 设备上做转换、精度或延迟验证。
- 当前结果只证明该候选在已反复用于调参的 validation 上略胜基线，不证明新视频上的泛化提升。旧 test split 未再次评估；要验证测试 mAP 是否超过 0.7，需新采集、按源视频隔离且从未参与训练或调参的数据。
- YOLO11m best 已导出固定输入 1×3×640×640、opset 17 的 ONNX：`model_export/artifacts/yolo11m_grouped_classroom_behavior_best.onnx`（76.7 MB）；checkpoint/ONNX 哈希在 `model_export/artifacts/yolo11m_grouped_export_manifest.json`。同一 grouped validation 用 Ultralytics ONNX Runtime CPUExecutionProvider 评估为 mAP50=0.7053、mAP50-95=0.5236；相对 PyTorch GPU 结果为 -0.0089/-0.0109，说明转换后有小幅精度损失。ONNX CPU 推理约 117.2 ms/图；本机 ONNX Runtime CUDA provider 因缺少所需 CUDA/cuDNN 动态库未能运行，随后已恢复 Windows 环境声明的 `onnxruntime==1.20.1`。板端 ATC/OM 及 Atlas 实测仍未执行。
- 为增加一个真正未参与训练的外部核验，从 Roboflow v5 source test split 准备了 3 类 remap，并排除与 source train/valid、全部 baseline grouped split 及外部 test 内部的 dHash≤4 近重复。原 test 1,333 张中排除了 1,221 张与 reference 重复、8 张外部 split 内重复，保留 104 张图；其中仅 27 张带目标标签，共 217 个目标（举手37、阅读111、书写69）。审计与数据说明见 `model_train/datasets_external_evaluation/classroom_behavior/`。
- 对这 104 张外部 holdout 做一次性配对评估：YOLO11m mAP50=0.2298、mAP50-95=0.1413；YOLO11s mAP50=0.2225、mAP50-95=0.1413。两者均远低于主数据集验证结果，YOLO11m 仅有约 +0.007 mAP50 的差别，不能视作泛化达到 0.7。该外部集样本少、图片域和类别定义可能与主集不同，结果作为跨数据集诊断，不作为模型选择反馈；完整报告分别在 `model_train/runs_grouped/validation/external_holdout/classroom_behavior_test_20261018_064818/`（YOLO11m）和 `.../classroom_behavior_test_20261018_064846/`（YOLO11s）。旧 grouped test 未再次读取。

## 第四轮：SCB5 同类数据补充微调（2026-10-18）

- 数据来源为作者发布的 [SCB5-Handrise-Read-write-2024-9-17](https://huggingface.co/datasets/wintonYF/SCB-Dataset/tree/main/SCB5-Handrise-Read-write-2024-9-17)，原标签为 `hand-raising/read/write`。作者仓库说明仅限学术研究或个人学习、非商业用途，并限制再分发；原始数据保留在仓库外的 `external-data/`，项目不分发原图。重复过滤、类别/标签质量审计脚本为 `model_train/12-audit-scb-dataset.py`。
- 用 64 位 dHash 汉明距离 ≤4 检查 SCB5 train/val 与全部 grouped train/valid/test。SCB5 train 的 5,193 张图中过滤了 3,490 张与现有任一 split 的近重复图，另去掉 875 张 SCB5 train 内重复图，留下 828 张新训练图、5,434 个去重目标框。SCB5 val 的 1,671 张图中过滤了 1,176 张与现有 split 或 SCB5 train 重复的图、302 张内部重复图，得到 193 张图、1,255 个目标框（举手189、阅读676、书写390）。
- 原始 SCB5 标签含 456/1,068 条重复框（train/val）和 4 个无效框；派生数据删除重复框并丢弃无效框。筛出的 train/holdout 均不与原 grouped train/valid/test 或彼此 dHash≤4 重复。训练列表是原 grouped train 3,317 张加 828 张新增图；训练选权重只用原 grouped validation 711 张。
- 从 `yolo11m_grouped_classroom_behavior_best.pt` 初始化，AdamW、640 输入、batch 4、lr0=0.0001、warmup_bias_lr=0.0001、patience=8；第 13 轮早停，保存第 5 轮 best。运行目录 `model_train/runs_grouped/train/yolo11m_scb5aux_v2_classroom_behavior/`，发布权重和训练参数在 `model_train/train_result_grouped/yolo11m_scb5aux_v2_classroom_behavior_*`。
- 与原 YOLO11m 在相同 grouped validation（640、batch 4、RTX 4060）独立复评：

  | 指标 | 原 YOLO11m | SCB5 微调候选 | 差值 |
  | --- | ---: | ---: | ---: |
  | mAP50 | 0.71417 | **0.71795** | +0.00378 |
  | mAP50-95 | 0.53458 | **0.53821** | +0.00363 |

  分类别 AP50：举手 0.78322→0.79069，阅读 0.69413→0.69679，书写 0.66517→0.66637。独立复评报告：`model_train/runs_grouped/validation/scb5_finetune_final/classroom_behavior_val_20261018_074740/`。
- 在 SCB5 193 张留出图上，冻结原 YOLO11m 的 mAP50/mAP50-95 为 0.54726/0.36839；微调候选为 0.60681/0.43150（+0.05955/+0.06312），三个类别 AP50 均提高。配对报告分别在 `model_train/runs_grouped/validation/scb5_external/classroom_behavior_test_20261018_070343/` 和 `.../classroom_behavior_test_20261018_074832/`。这项结果说明对该 SCB5 样本的跨拆分迁移有所改善，但分数仍低于 0.7；该留出结果已用于候选决策，不应再拿它反复筛模型，也不代表其它学校/视频。
- 候选已导出固定输入 1×3×640×640、opset 17 ONNX：`model_export/artifacts/yolo11m_scb5aux_v2_classroom_behavior_best.onnx`。Ultralytics ONNX Runtime CPUExecutionProvider 在 grouped validation 得到 mAP50=0.71165、mAP50-95=0.52988，相对 PyTorch 低 0.00630/0.00833；CPU 推理约 138.8 ms/图。ONNX provenance 在 `model_export/artifacts/yolo11m_scb5aux_v2_export_manifest.json`。
- 因而目前最好的验证候选已更新为 `yolo11m_scb5aux_v2`； grouped validation 超过 0.7，SCB5 同类留出样本从 0.547 提高到 0.607，但尚未证明新学校真实视频达到 0.7。若需作最终泛化声明，仍需另采从未参与训练或挑选的目标域测试数据。原 grouped test 未再次评估。Atlas 端转换和实测仍未做。

## 补充数据源审计（2026-10-18）

- 检查了 Roboflow Universe 的 [Handrise-Read-Write v1](https://universe.roboflow.com/studentclassroombehavior/handrise-read-write)，数据集页面列出 5,175 张图片并标注 CC BY 4.0。下载副本保存在仓库外 `external-data/rfaux_scb_hrr_v1`；不将原图复制进项目。
- v1 划分为 train 3,623、valid 1,034、test 518，导出类别名只有数字 `0/1/2`；train 标签结构检查发现 0 个无效框，valid 有 2 个无效框，test 为 0。其类别映射没有可靠的文本定义，当前不作为训练数据。
- 用 dHash 汉明距离 ≤4 与项目 grouped train/valid/test 比较后，3,623 张训练图中有 2,458 张近重复，初筛剩 1,165 张。再与已使用的 SCB5 派生训练/留出图比较，其中 1,157 张近重复；合并去重后只有 8 张新增图。
- 由于有效增量极小且类别定义不够清楚，没有基于该数据启动训练。重合明细在仓库外 `external-data/rfaux_scb_hrr_v1/review/baseline_near_duplicates.csv`。下一轮应采集同目标域、不同课堂/机位/源视频的样本，重点覆盖 reading 与 writing，并按源视频隔离训练、验证、最终测试集合。

## 第五轮：新增课堂来源微调（2026-10-18）

- 选择 Roboflow Universe 的 [classroom-4sjvo v3](https://universe.roboflow.com/studentbehavior-37daj/classroom-4sjvo)，页面列出 428 张图、5 类（hand-raising、leaning over the table、reading、using phone、writing），许可为 CC BY 4.0。审计脚本为 `model_train/13-audit-classroom-dataset.py`；准备脚本为 `model_train/14-prepare-classroom4-train.py`。数据与派生图保留在仓库外 `external-data/rfaux_classroom_4sjvo_v3` 和 `external-data/classroom4_audited`。
- 将来源 train 428 张图与 grouped train/valid/test、SCB5 保留训练/诊断图、两批已用 Roboflow 数据共 23,054 张图进行 64-bit dHash 检查（Hamming ≤4）。24 张命中已用数据，均与 `studentdetection v5` 图像重合；其余图中 132 张含项目目标类别，共 203 个框：举手 88、阅读 87、书写 28。原始标注没有格式无效框或重复行。来源的 leaning/phone 类被排除；类别 id 0/2/4 映射到项目 0/1/2。抽样可视化与重合明细在来源目录 `review/`。
- 合并训练清单包括原 grouped train、SCB5 去重训练图及 132 张新增图，共 4,277 张；模型选择只用 grouped validation 711 张，未用 grouped test。
- 全层微调从 `yolo11m_scb5aux_v2` 初始化，AdamW、640、batch 4、lr0 0.0001；早停于第 10 轮，best 第 2 轮。head-only 微调从同一初始化开始，冻结 backbone、lr0 0.00005；早停于第 9 轮，best 第 4 轮。训练参数和 checkpoint SHA 在同名 `*_training.json`。
- 独立 grouped validation 同设置对照：

  | 模型 | mAP50 | mAP50-95 | 举手 AP50 | 阅读 AP50 | 书写 AP50 |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | SCB5 当前候选 | 0.71795 | 0.53821 | 0.79069 | 0.69679 | 0.66637 |
  | classroom-4 全层微调 | 0.72069 | 0.54149 | 0.79197 | 0.70070 | 0.66941 |
  | classroom-4 head-only | **0.72148** | **0.54189** | 0.79071 | **0.70081** | **0.67293** |

- 对已用于模型选择的 SCB5 193 张诊断集，head-only 为 mAP50=0.60591、mAP50-95=0.43356，相比 SCB5 当前候选 0.60681/0.43150 基本持平；全层候选为 0.60374/0.43108。该诊断集不是盲测。故 head-only 是目前 grouped validation 最高的实验 checkpoint，但没有证据证明它使跨域 mAP50 提高到 0.7；`yolo11m_scb5aux_v2` 仍作为较稳妥的推荐权重，新增候选暂不导出/替换部署文件。
- 两个新训练 checkpoint：`model_train/train_result_grouped/yolo11m_classroom4aux_v1_classroom_behavior_best.pt`（全层）和 `model_train/train_result_grouped/yolo11m_classroom4aux_headonly_v1_classroom_behavior_best.pt`（head-only）。评估报告分别在 `model_train/runs_grouped/validation/classroom4aux_candidate/`、`classroom4aux_headonly/` 和 `classroom4aux_scb5_diagnostic_headonly/`。
- 公开来源的更大候选 `student-behaviour-in-classroom-4` 虽列有 1,470 张图和相同目标类，但项目没有可下载的 dataset version；另一个含目标类别的仓库没有明确再利用许可。本轮未使用。继续将真实新视频按源视频隔离的目标域评估作为达到外部 mAP50≥0.7 的必要条件。

## 推理时增强（TTA）探针（2026-10-18）

- 为复核推理时多尺度/翻转增强能否补足精度，在 `model_train/4-validate-model.py` 增加 `--augment` 开关并记录于评估 JSON。没有改动权重；TTA 会增加推理时间。
- 使用相同的 640、batch 4、RTX 4060 设置比较 v2 和 head-only 两个 checkpoint：

  | checkpoint / 设置 | grouped val mAP50 / mAP50-95 | SCB5 诊断 mAP50 / mAP50-95 |
  | --- | ---: | ---: |
  | v2，普通推理 | 0.71795 / 0.53821 | 0.60681 / 0.43150 |
  | v2，TTA | 0.72605 / 0.54991 | 0.61114 / 0.43905 |
  | head-only，普通推理 | 0.72148 / 0.54189 | 0.60591 / 0.43356 |
  | head-only，TTA | **0.73104 / 0.55432** | **0.61426 / 0.44101** |

- head-only 的 grouped validation TTA 推理时间约 39.0 ms/图；普通推理约 16.8 ms/图（仅模型 inference 部分，不含解码和业务处理）。SCB5 诊断集已参与之前的模型开发，不能视为盲测；其 TTA 分数仍低于 0.7。TTA 目前是一个较高精度、较低吞吐率的选项，不代表新目标课堂已验证。
- 报告路径：`model_train/runs_grouped/validation/tta_headonly_val/`、`tta_v2_val/`、`tta_headonly_scb5_diagnostic/`、`tta_v2_scb5_diagnostic/`。

## 第六轮：课堂行为数据源去重审计（2026-10-18）

- 检查 Roboflow Universe 的 [Student-behaviour-in-classroom-2 v1](https://universe.roboflow.com/studentclassroomanalytics/student-behaviour-in-classroom-2/dataset/1)，页面标注 CC BY 4.0、类别为 reading / hand-raising / writing。使用本机用户级 `ROBOFLOW_API_KEY` 下载至仓库外 `external-data/rfaux_classroom_behavior_2_v1`；密钥未写入文件或输出。该导出 `data.yaml` 仅含数字类名，因此审计不依赖未经核实的类 ID 顺序，也没有用它训练。
- 新审计脚本 `model_train/15-audit-classroom2-dataset.py` 将其 1,032 张 train 图与项目 grouped 三个 split、保留的 SCB5 图和之前审计过的 Roboflow 来源做 dHash 汉明距离 ≤4 比较。1,032 张都命中已有图；按每张图的最近匹配距离统计，955 张为 0、72 张为 1、4 张为 2、1 张为 3。故本轮可用新增训练图为 0，未启动训练。
- 完整来源命中清单与 JSON 报告保存在仓库外 `external-data/rfaux_classroom_behavior_2_v1/review/`。来源数据不复制进项目。
- 这次审计没有改变模型或精度结论：当前 head-only + TTA grouped validation mAP50 为 0.73104；已用于开发的 SCB5 诊断集为 0.61426。达到新课堂数据 mAP50≥0.7 仍需真实目标域、按源视频隔离且从未参与调参的标注数据。阅读/书写交叉错误样例中存在单帧难以判别的动作，人工复核并统一标注定义应先于更多相似数据微调。

## 权重插值探针（2026-10-18）

- 在不覆盖正式 checkpoint 的前提下，将 SCB5 v2 与其 head-only 微调候选的浮点权重按 α=0.25、0.5 插值；脚本为 `model_train/16-make-weight-soup.py`，输出留在 `model_train/runs_grouped/weight_soup/`。两者来自同一初始化，形状与张量类型相同。
- 以相同 640 输入、TTA、batch 4 评估：

  | 候选 | grouped validation mAP50 / mAP50-95 | SCB5 诊断 mAP50 / mAP50-95 |
  | --- | ---: | ---: |
  | head-only，未插值 | **0.73104 / 0.55432** | **0.61426 / 0.44101** |
  | 插值 α=0.5 | 0.72924 / 0.55281 | 0.61352 / 0.44012 |
  | 插值 α=0.25 | 0.72770 / 0.55188 | 0.61280 / 0.43884 |

- 插值没有改善任一数据集上的指标，故不取代 head-only TTA 候选。SCB5 是已用于开发的诊断集，仍不代表独立新课堂泛化；结果和运行产物保留在 `model_train/runs_grouped/validation/weight_soup*`。

## 阅读 / 书写人工复核材料（2026-10-18）

- 将既有 344 个 reading↔writing 高重叠错分候选制作成 22 张局部放大 review sheet，青色框为数据标注、红色框为模型预测；对应索引 `index.csv` 可映射回原 review CSV 与全图。脚本为 `model_train/17-build-read-write-review-sheets.py`，图页位于 `model_train/dataset_analysis/classroom_behavior/reading_writing_review_sheets/`。
- 仅生成核查材料，没有自动重标或更改任何数据。复核操作与建议判定项见 `model_train/dataset_analysis/classroom_behavior/READ_WRITE_REVIEW_GUIDE.md`。经人工确认后才能修正训练标签并重训；如果标签定义本身无法凭单帧判定，应保留为歧义样例，而非把模型预测当作真值。

## NMS 阈值调优（2026-10-18）

- 对 head-only checkpoint 在 grouped validation 上扫描 NMS IoU 阈值 0.40、0.45、0.50、0.60、0.80。TTA 模式下 0.50 最佳：mAP50=0.73764、mAP50-95=0.54985；相同 TTA、默认 0.70 是 0.73104/0.55432。对未启用 TTA 的推理，0.50 为 0.72471/0.53779，默认 0.70 为 0.72148/0.54189。即 mAP50 有小幅提高，同时 mAP50-95 略降。
- 在已用于开发的 SCB5 193 张诊断集上，TTA + IoU 0.50 得到 0.62269/0.43864；TTA + 0.70 为 0.61426/0.44101。普通推理 + 0.50 得到 0.60941/0.43302；普通推理 + 0.70 为 0.60591/0.43356。跨域诊断显示 mAP50 小幅上涨，但数据已参与开发，不能视为盲测，也仍未达到 0.7。
- 因此对当前 PyTorch 推理候选，若首要指标是 mAP50，可显式使用 `--iou 0.50`；TTA 会明显增加推理耗时。未修改模型权重或 C++/ONNX 部署默认值；部署后处理需在对应格式与硬件上单独验证。运行结果位于 `model_train/runs_grouped/validation/nms_iou_*` 与 `nms_plain_iou_*`。
- 在相同 IoU=0.50 + TTA 下增加输入尺寸探针：768 在 grouped validation 为 0.74064/0.55383、SCB5 诊断集为 0.63053/0.45581，验证推理约 49 ms/图；832 grouped validation 为 0.73635/0.54972；896 grouped validation 降至 0.73125/0.54105，但 SCB5 诊断升至 0.64131/0.46939，验证推理约 66 ms/图。因 896 的收益只体现在已用于开发的 SCB5 诊断集，且项目验证集明显回退，不以它替代 768。当前 PyTorch 精度优先配置候选为 **head-only + 768 + TTA + NMS IoU 0.50**；SCB5 诊断仍未达到 0.7，新课堂泛化尚未证明。

## Head-only 768 ONNX 导出（2026-10-18）

- 使用 `model_export/export_onnx.py` 将 head-only checkpoint 导出为固定输入 `1×3×768×768`、opset 17 的 ONNX：`model_export/artifacts/classroom4aux_headonly_768/yolo11m_classroom4aux_headonly_v1_classroom_behavior_best.onnx`。来源 checkpoint SHA-256 为 `f528401493ab6d9ff93acb95d79fa3b7b2d8e0f4ed3a774874a8a8d87d6f1149`；ONNX SHA-256 为 `db8f5ce4c72d178a8d2b4f0435dc101ece9fa6d6b793922cb051e38b77cbd396`。完整 provenance 在同目录 `yolo11m_classroom4aux_headonly_v1_export_manifest.json`。
- 用 Ultralytics ONNX Runtime CPUExecutionProvider、普通推理、NMS IoU=0.50 做固定输入评估：grouped validation mAP50/mAP50-95 为 0.70596/0.51166，SCB5 开发诊断集为 0.61785/0.44572，CPU 推理约 266–285 ms/图。
- 同一数据/尺寸/NMS、PyTorch 普通推理对照分别为 validation 0.71265/0.52110、SCB5 诊断 0.62592/0.45534；ONNX 差值为 -0.00668/-0.00944 和 -0.00807/-0.00961。此 ONNX 不包含 TTA，因此不能与 PyTorch 768 + TTA 的 0.74064/0.55383 直接比较。
- 该实验 ONNX 尚未替换 `yolo11m_scb5aux_v2` 的推荐导出，也未生成 OM、在 Atlas DK A2 上转换或实测。CPU 速度不代表板端速度；若要端侧交付，仍需在目标板上完成 ATC、输出后处理适配、精度和延迟实测。

## ONNX 部署评估口径复核（2026-10-18）

- 修复 `model_val/accuracy_eval.py` 对 Ultralytics `.txt` 图片列表的读取；此前会把 `valid.txt` 本身误当图片，未能完成评估。
- 将 AP 统计改为调用 Ultralytics 原生 `ap_per_class` / `box_iou`；NMS 改为先在网络输入坐标做，再逆变换；保留越界预测框参与 IoU。修复前的边缘样例会少检 1 个 reading 框，修复后该图 51 个检测框与 Ultralytics 一致。
- 固定输入、阈值与数据清单重新评估后，自定义评估器仍得到 grouped validation mAP50/mAP50-95=0.68254/0.49284；既有 Ultralytics validator 报告为 0.70596/0.51166。另一次逐图 NMS 对照中 711 张的检测总数相同，3 张存在类别内排序/坐标差异。差值仍未完全解释，因此 ONNX 精度的正式数字暂采用 Ultralytics validator；自定义部署评估结果不作为最终精度声明。
- 这次复核没有修改模型权重；Ultralytics 的 0.70596 只代表 grouped validation，不代表新课堂泛化。要确认部署端指标，还需将逐图预测与 validator 的 TP/FP 统计一一对照，并在目标 Atlas 设备验证实际后处理。
- 恢复虚拟环境依赖到锁定版本 `onnxruntime==1.20.1`、`numpy==2.3.1`、`sympy==1.13.1`。本轮未改动模型权重。

## 第七轮：Roboflow 新课堂数据候选审计（2026-10-18）

- 检索到 [Student Behaviour Detection v6](https://universe.roboflow.com/mywork-lkwz4/student-behaviour-detection-neazg/dataset/6)，页面标记 CC BY 4.0，列出 4,065 张图；导出的 12 类中 `hand-raising`、`reading`、`writing` 分别映射为 class ID 4、7、11。下载到仓库外 `external-data/rfaux_student_behaviour_neazg_v6`，密钥只从 Windows 用户环境读取。
- 首次大文件下载在中途超时；改进 `model_train/6-download-aux-dataset.py`，加入 HTTP Range 断点续传及错误脱敏后完成下载和解压。没有把数据或密钥放进仓库。
- `model_train/18-audit-neazg-v6.py` 对 4,065 张图与 grouped 三个 split、已使用 SCB5、HRR、studentdetection v5、classroom4 v3、classroom behavior 2 v1 做 64-bit dHash，阈值为 Hamming≤4；也检查源数据内部重复、标签字段、边界和重复框。标签格式无错误，重复标签行为 0。
- 3,192 张 train 图中 2,909 张匹配已有项目数据，1,862 张还与来源 valid/test 近重复；原 train split 最终只剩 16 张含目标类的独立图和 52 个 reading 框。将全部来源 split 合并、再与既有数据去重并剔除内部近重复后，候选训练池为 47 张独立目标图、171 个 reading 框；hand-raising 和 writing 新增框均为 0。
- 有效增量太少且类别不平衡，本轮不启动微调。完整审计与候选清单位于仓库外 `external-data/rfaux_student_behaviour_neazg_v6/review/`；不使用这个来源的原 valid/test split 做模型评分。

## 第八轮：Class Monitoring 候选数据审计（2026-10-18）

- 检查 Roboflow Universe 的 [Class Monitoring v4](https://universe.roboflow.com/noumancs192043/class-monitoring/dataset/4)，页面列出 2,677 张图并标注 CC BY 4.0。类别映射为 hand-raising=3、reading=4、writing=8；数据保存在仓库外 `external-data/rfaux_class_monitoring_v4`。
- 用 `model_train/18-audit-neazg-v6.py --source-root external-data/rfaux_class_monitoring_v4` 对来源数据和已有项目图像做 dHash（Hamming≤4）及标签检查。train/valid/test 共 2,677 张，发现 1 条重复标签行；train 1,640 张中 1,485 张匹配既有数据，全部 split 中 2,342 张匹配既有数据。
- 来源全部 split 合并并去重后，只保留 10 张独立目标类图片：1 个 hand-raising、99 个 reading、34 个 writing 框；来源内部另移除 325 张近重复图。新增量太少且举手几乎缺失，不用于训练。报告和明细保存在仓库外 `external-data/rfaux_class_monitoring_v4/review/`。
- 因而继续下载近似公开集无法有效补足差距。下一步应集中处理现有 reading↔writing 混淆样例，按统一标注定义人工复核；若要确认目标课堂 mAP50≥0.7，需要收集从未参与训练或调参、按源视频隔离的真实目标域测试集。

## 第九轮：阅读 / 书写错分复核与训练标签小规模修正（2026-10-18）

- 将验证集 344 个高 IoU 阅读↔书写错分候选逐一对照完整原图；确认这 344 张全部来自 grouped validation（train/test 命中数均为 0），所以未将它们用于训练，也未改动正式验证标签。按“阅读需能看出注意文本；书写需能看出正在写字”的操作性规则，复核记录为：40 个疑似错类、62 个支持原标注、242 个单帧歧义。复核清单：`model_train/dataset_analysis/classroom_behavior/reading_writing_review_sheets/manual_review_progress.csv`。这只是针对模型错分筛出的样本，不能代替随机抽样或目标域盲测。
- 为寻找可用于训练的修正样例，新增训练集 in-sample 诊断：现有 head-only checkpoint 在 grouped train 3,317 张图上 mAP50/mAP50-95=0.89769/0.75118；这是训练内成绩，不代表泛化。诊断发现 2,114 条阅读↔书写高 IoU 混淆。`model_train/7-analyze-validation-errors.py` 新增 `--split-name`，避免把 train 诊断错报为 validation。
- 对训练集错分候选抽查 96 条（37 个阅读→书写疑似错标、6 个支持原书写标注、53 个歧义）；用 bbox 与原始多边形标签逐一精确匹配，37 条修正全部达到 IoU=1.0，分布在 31 张 grouped-train 图。创建独立训练副本 `model_train/dataset_analysis/training_error_audit/corrected_train/`；正式源数据、grouped validation、grouped test 均未修改。
- 两轮 head-only 小规模微调对照（均从当前推荐 checkpoint 初始化，原 validation 不变）：

  | 实验 | 手工修正标签数 | grouped val mAP50 / mAP50-95 | SCB5 开发诊断 mAP50 / mAP50-95 |
  |---|---:|---:|---:|
  | 当前推荐 checkpoint（768 + TTA + IoU .50） | — | 0.74064 / 0.55383 | 0.63053 / 0.45581 |
  | manual24 | 24 | 0.73820 / 0.55385 | 0.63163 / 0.45293 |
  | manual37 | 37 | 0.73979 / 0.55460 | 0.62961 / 0.45186 |

- 两个候选都没有整体超过当前推荐模型，未取代原 checkpoint。manual37 权重与报告保留在 `model_train/train_result_grouped/yolo11m_manual37_headonly_v2_*`、`model_train/runs_grouped/validation/manual37_candidate/` 和 `manual37_candidate_scb5_diagnostic/`。SCB5 已参与开发，不是盲测；当前仍无证据表明目标课堂 mAP50≥0.7。
- 本轮说明错标是数据质量线索，但 37 个修正确认样本不足以改变泛化表现。下一步最有效的是新增目标课堂的多机位/多源视频并按源视频隔离标注；目前可用公开候选高度重复，未发现足够独立新增样本。

## 第十轮：更高输入分辨率探针（2026-10-18）

- 从当前正式 checkpoint `yolo11m_classroom4aux_headonly_v1` 出发，固定 TTA、NMS IoU=0.50、batch=1，仅改变输入尺寸，在 SCB5 193 张已参与开发的诊断集和 grouped validation 711 张上评估 960 与 1024。该诊断集用于探索性调参，不能作为盲测结论。

  | 输入尺寸 | SCB5 mAP50 / mAP50-95 | grouped validation mAP50 / mAP50-95 | SCB5 阅读 AP50 | SCB5 书写 AP50 |
  |---:|---:|---:|---:|---:|
  | 896（此前最佳尺寸探针） | 0.64131 / 0.46939 | 0.73125 / 0.54105 | 0.58022 | 0.58856 |
  | 960 | **0.66057 / 0.48598** | **0.73161 / 0.53682** | **0.59464** | **0.60451** |
  | 1024 | 0.65715 / 0.48079 | 0.72857 / 0.53089 | 0.59209 | 0.59248 |

- 960 相比 896 的 SCB5 mAP50 提高 0.01926，阅读和书写也有小幅提升；相对 768 的 0.63053 提高 0.03004。但 SCB5 仍未达到 0.7，且 grouped mAP50 比 768 的 0.74064 低约 0.009。1024 两项数据集上都不如 960。960 TTA 平均模型推理耗时约 48.4 ms/图。
- 这些实验没有改动训练数据或 checkpoint。960 是现阶段 SCB5 开发诊断下的推理候选，不取代正式默认配置；若看重现有 grouped validation，768 仍更高。新课堂盲测仍未建立。报告分别在 `model_train/runs_grouped/validation/tta_iou05_960_probe_scb5_diag/`、`tta_iou05_960_probe_grouped/`、`tta_iou05_1024_probe_scb5_diag/`、`tta_iou05_1024_probe_grouped/`。

## 第十一轮：DataMountainSCB 新来源审计与一次性跨来源评估（2026-10-18）

- 发现新的公开来源 [DataMountainSCB v1.0.0](https://zenodo.org/records/15009521)，对应 [GitHub 发布仓库](https://github.com/Chunyu-Dong/DataFountainSCB1)。Zenodo 元数据为开放获取、CC BY 4.0，记录 DOI 10.5281/zenodo.15009521。数据只放在仓库外 `external-data/datamountain_scb_v1/`；下载的 9 个 ZIP 分卷均通过 `zipfile.testzip()`，按 YOLO 标签结构整理为 425 train / 73 test。
- 按发布的 6 类映射（0 hand-raising、1 reading、2 writing、其余 phone/bowing/leaning）审核 498 张图：标签格式问题 0、重复标签行 0。训练 split 原含 171 举手、4,158 阅读、1,526 书写框；来源 test 含 0 举手、1,128 阅读、334 书写框。
- 用项目现有 dHash 审计脚本，汉明距离阈值≤4，与 grouped train/valid/test、SCB5 及已审计辅助来源对照。全来源 50 张命中已有数据；425 张来源 train 中有 283 张与来源 test 近重复、87 张额外属于来源 train 内近重复，逐图去重和隔离 test 后仅 5 张独立新训练图（50 阅读、15 书写、0 举手）。即使将所有来源 split 当候选池，也只有 12 张独立新图（120 阅读、35 书写、0 举手），因此不加入训练。审计报告与对应关系在仓库外 `external-data/datamountain_scb_v1/normalized/review/novelty_audit.json`、`train_near_duplicates.csv`。
- 对该来源原 test 中未命中已有项目图片的 73 张图做一次性评估，固定当前正式模型、768 输入、TTA、NMS IoU 0.50。mAP50/mAP50-95 为 **0.29995 / 0.16770**；阅读 AP50=0.38778，书写 AP50=0.21212。该 split 无举手标注，因此总体 mAP 仅对阅读和书写两类取均值。该来源 test 已用于报告此诊断，不再用于训练或挑选 checkpoint。结果位于 `model_train/runs_grouped/validation/datamountain_independent_source/classroom_behavior_test_20261018_132437/`。
- 该低分是新的跨来源诊断证据，不等于目标课堂的表现；不过它表明当前模型仍有明显域差异，也不能把 SCB5 的 0.66 当作普遍泛化。当前没有足够的新来源训练图来缩小差距，达到真实目标课堂 mAP50≥0.7 仍需独立目标课堂视频与按视频隔离的人工标注。

## 目标课堂素材入口准备（2026-10-18）

- 在项目目录及常用视频/下载/桌面/文档目录按路径与文件名检查，未发现可确认属于课堂的原始视频；找到的普通视频素材没有按课堂关键词命名，未据此推断其内容。
- 新增 `model_train/19-prepare-target-classroom-videos.py`：对仓库外提供的视频递归抽帧，默认每 2 秒采样，按视频 SHA-256 生成稳定 `source_video_id`，并记录帧号、时间戳和图像尺寸；不自动标注、不随机拆帧，输出目录已有文件时拒绝覆盖。
- 新增 `TARGET_DOMAIN_DATA.md`，列出采集入口、运行命令、reading/writing/hand-raising 操作性规则、视频级 train/validation/test 隔离要求和预期标注结构。视频素材到位后，再人工标注并按整段源视频划分；之前评估过的 DataMountain test 不回流训练。

## DataMountainSCB 训练集微调探针（2026-10-18）

- 构建仓库外训练副本，只取 DataMountainSCB train；逐图排除与 grouped validation/test 的 dHash≤4 近重复（19 张），然后仅移除完全相同 dHash 的内部重复帧（259 张），保留 147 张辅助训练图，目标框为举手 103、阅读 1,235、书写 482。DataMountain 原 test split 未加入训练。
- 从正式 head-only checkpoint 初始化，按同一 head-only AdamW 方案微调；第 6 轮早停，训练内 grouped validation 最佳为第 1 轮。候选 checkpoint SHA-256：`cbdc23c0f5881c321bc967c2cb8797a9040f56c6caaa33193811b829d0f07bf1`。准备报告在仓库外 `external-data/datamountain_scb_v1/prepared_training_exact/preparation_report.json`；训练副本和源图片不入库。
- 固定 768 + TTA + IoU 0.50 重评：

  | 模型 | grouped validation mAP50 / mAP50-95 | SCB5 开发诊断 mAP50 / mAP50-95 |
  |---|---:|---:|
  | 正式模型 | 0.74064 / 0.55383 | 0.63053 / 0.45581 |
  | DataMountain147 head-only | 0.73868 / 0.55322 | 0.63268 / 0.45506 |

- SCB5 mAP50 增加 0.00215，但 mAP50-95 降低；grouped validation 两项也略降。该变化太小且方向不一致，不替换正式 checkpoint。DataMountain 来源测试集已在上轮用于一次性评估；本轮及后续未再次评估它，也不把它作为泛化成绩。
- 运行产物：`model_train/runs_grouped/train/yolo11m_datamountain147_headonly_v1_classroom_behavior/`、`model_train/runs_grouped/validation/datamountain147_candidate_val/`、`model_train/runs_grouped/validation/datamountain147_candidate_scb5_diagnostic/`。候选权重和训练记录位于 `model_train/train_result_grouped/yolo11m_datamountain147_headonly_v1_classroom_behavior_*`。

## 第十二轮：1152 输入尺寸探针（2026-10-18）

- 按第十轮相同口径，用正式 checkpoint、TTA、NMS IoU=0.50、batch=1 评估 1152 输入；诊断仍使用已参与开发的 SCB5，另在 grouped validation 确认项目内表现。未训练或替换权重。

  | 输入尺寸 | SCB5 开发诊断 mAP50 / mAP50-95 | grouped validation mAP50 / mAP50-95 |
  |---:|---:|---:|
  | 960 | 0.66057 / 0.48598 | 0.73161 / 0.53682 |
  | 1024 | 0.65715 / 0.48079 | 0.72857 / 0.53089 |
  | 1152 | 0.65233 / 0.48054 | 0.70942 / 0.51068 |

- 1152 两个数据集都弱于 960；相对 960，SCB5 mAP50 下降 0.00824，grouped validation 下降 0.02219。故停止继续放大输入尺寸，768 仍是项目验证集精度优先配置，960 仅是已见 SCB5 诊断成绩较高的实验设置，不改正式推荐权重或默认尺寸。
- 常用目标视频入口 `external-data/target_videos` 当前不存在。没有新目标域素材时，继续盲目调参无法证明真实课堂 mAP50 达到 0.7；下一阶段将优先使用目标课堂视频按源视频隔离标注，随后再做训练和留出评估。运行报告位于 `model_train/runs_grouped/validation/tta_iou05_1152_probe_scb5_diag/` 与 `tta_iou05_1152_probe_grouped/`。

## 第十三轮：ClaBehavior 来源审计（2026-10-18）

- 发现 [CCNUZFW/Student-behavior-detection-system 的 ClaBehavior COCO 数据](https://github.com/CCNUZFW/Student-behavior-detection-system/tree/master/dataset/coco)。公开仓库论文卡片声称 1,342 张，但实际训练标注文件包含 360 张 train、7,250 个框；目标类为 write/read/raise_hand。只下载 train 图像 360 张，未下载或使用来源 val 图片。
- 审计脚本 `model_train/21-audit-clabehavior.py` 将候选与 grouped 三个 split、SCB5、已审计 Roboflow 来源和 DataMountain 共 32,197 张图片按 dHash Hamming≤4 比对；候选跨来源近重复为 0，但来源内部存在大量近重复（8,507 对）。按固定顺序对每个近重复邻域保留一个代表图后，训练候选为 117 张，新增目标框为举手 164、阅读 197、书写 64。COCO bbox 检查问题 0、重复标注行 0。
- 代码仓库的 LICENSE 为 Apache-2.0，但没有找到数据图片的单独许可说明。数据及本地训练产物均保存在仓库外，不做分发；如需公开发布数据或衍生权重，先取得数据许可确认。
- 近重复审计报告、图像匹配表、候选清单保存在仓库外 `external-data/clabehavior_audit/review/`。辅助数据只取来源 train；项目 grouped val/test 保持原样。

## 第十四轮：ClaBehavior117 head-only 微调（2026-10-18）

- 从正式 head-only checkpoint 初始化，使用 117 张去重的 ClaBehavior train 图像进行 head-only 微调；最多 15 epoch，patience=5，第 10 轮早停。项目训练 split 与 grouped validation/test 清单按既有流程保留。候选 SHA-256 记录在 `model_train/train_result_grouped/yolo11m_clabehavior117_headonly_v1_classroom_behavior_training.json`。
- 同口径复评（768 输入、TTA、NMS IoU=0.50）：

  | 模型 | grouped validation mAP50 / mAP50-95 | SCB5 开发诊断 mAP50 / mAP50-95 |
  |---|---:|---:|
  | 正式模型 | 0.74064 / 0.55383 | 0.63053 / 0.45581 |
  | ClaBehavior117 候选 | 0.73808 / 0.55218 | 0.62659 / 0.45225 |

- 候选在 960 + TTA + IoU 0.50 下，SCB5 诊断为 0.65576 / 0.48108；正式模型为 0.66057 / 0.48598。三项比较均未超过正式模型，未替换正式权重。SCB5 是已用于开发的诊断集，不能视为新课堂盲测。
- 训练/评估产物位于 `model_train/runs_grouped/train/yolo11m_clabehavior117_headonly_v1_classroom_behavior/`、`model_train/runs_grouped/validation/clabehavior117_candidate_val/`、`clabehavior117_candidate_scb5_768/` 和 `clabehavior117_candidate_scb5_960/`。当前目标课堂 mAP50≥0.7 仍未验证；下一步增益需要真实目标课堂视频和视频级隔离标注。

## 第十五轮：双模型推理融合探针（2026-10-18）

- 以正式 head-only checkpoint 为主模型，分别与 grouped-only checkpoint、其 SCB5 微调父 checkpoint 组成双模型输出拼接/NMS ensemble；普通推理、768 输入、IoU=0.50，仅在已参与开发的 SCB5 193 张诊断集评估。没有替换单模型推理配置。

  | 推理模型 | SCB5 诊断 mAP50 / mAP50-95 | 正式模型普通推理基线 |
  |---|---:|---:|
  | 正式模型 | 0.62592 / 0.45534 | — |
  | 正式 + grouped-only | 0.61460 / 0.43423 | 下降 |
  | 正式 + SCB5 父模型 | 0.62552 / 0.45386 | 基本持平，略低 |

- 简单拼接/NMS 没有跨过单模型基线，停止这条融合路线。`23-evaluate-model-ensemble.py` 保留为普通推理探针；Ultralytics 对自定义 ensemble 忽略 TTA 请求，相关运行记录已更正为普通单次推理，不能据此声称评估了 ensemble+TTA。
- 目标课堂视频入口仍为空。当前能确认的最高 SCB5 开发诊断 mAP50 为正式模型 960 + TTA 的 0.66057；模型训练和推理策略的公开/项目内候选均未达到 0.7。达到真实目标课堂 0.7 仍须依靠目标课堂样本和按源视频隔离的验证/测试。
- 运行结果位于 `model_train/runs_grouped/validation/ensemble_official_groupedbase_scb5/plain_768/`、`model_train/runs_grouped/validation/ensemble_official_scb5parent_scb5/plain_768/`。TTA 请求被忽略的辅助日志在同目录 `tta_768/`，该记录已标明其实际为普通推理。

## 第十六轮：重叠分块推理探针（2026-10-18）

- 对正式 checkpoint 在 SCB5 开发诊断集测试全帧与重叠分块推理；每个 crop 使用 Ultralytics `model.predict`，合并 crop 坐标后做全图 class-wise NMS，再用 Ultralytics `DetMetrics` 统计。该路径与 `model.val` 使用不同的 inference wrapper，故绝对数值不与官方 validator 直接混用；比较仅看同一脚本下全帧/分块相对变化。

  | imgsz / 源 crop | 推理方式 | SCB5 mAP50 / mAP50-95 | precision / recall | 平均 tile 数 |
  |---:|---|---:|---:|---:|
  | 768 / 全帧 | TTA | 0.60946 / 0.43814 | 0.570 / 0.618 | 1.00 |
  | 768 / 768 | 重叠 25% + TTA | 0.57067 / 0.38041 | 0.508 / 0.650 | 2.63 |
  | 960 / 全帧 | TTA | 0.63500 / 0.46792 | 0.583 / 0.660 | 1.00 |
  | 960 / 960 | 重叠 25% + TTA | 0.60027 / 0.42678 | 0.573 / 0.628 | 1.62 |

- 两种 crop 尺寸的分块 mAP 都低于同脚本全帧；召回提升没有抵消 precision/AP 的下降。对照官方 validator，正式模型的 768/960 TTA 分别为 0.63053/0.45581 与 0.66057/0.48598；该探针全帧与官方分数存在约 0.02–0.03 的绝对差异，因此不把其绝对 AP 当作官方报告，也不采用分块推理配置。
- 探针脚本 `model_train/24-evaluate-tiled-inference.py` 和结果保留在 `model_train/runs_grouped/validation/fullframe_custom_metric_check_2/`、`fullframe_custom_metric_plain/`、`fullframe960_custom_metric/`、`tile768_overlap25_scb5/`、`tile960_overlap25_scb5/`。未修改 checkpoint。目标课堂视频仍未提供，真实目标场景 mAP50≥0.7 仍未证实。

## 第十七轮：SCBehavior 数据源可用性核查（2026-10-18）

- 核查公开仓库 [CCNUZFW/SCBehavior](https://github.com/CCNUZFW/SCBehavior)，README 声称 1,346 张图并含 read、write、raise_hand 等行为，但 GitHub 当前树实际只有 400 个唯一文件路径样本（360 train、40 val；COCO 与 YOLO 两套格式重复列出），和 README 数量不符。
- 当前仓库中的 800 个图像路径都是 Git LFS 指针文件；普通 Git/raw 文件读取到的是指针，尚未验证 LFS 对象能否完整取得。仓库未找到独立 LICENSE 文件。引用论文页面的 CC BY 3.0 不能直接确认数据图像本身的再利用许可，因此本轮没有下载 LFS 图像或加入训练。
- 结论：该来源当前不具备可核实的数据规模、可直接取得的图片内容和明确的数据授权，不进入模型实验。目标视频目录 `external-data/target_videos` 当前仍不存在；在视频到位前，grouped validation 只能用于模型内比较，不能证明新课堂 mAP50≥0.7。

## 第十八轮：UCB-Dataset v2 新来源核验（2026-10-18）

- Mendeley Data 的 [UCB-Dataset v2](https://data.mendeley.com/datasets/pz7y4bpfxy/2) 记录列为 CC BY 4.0，公开 API 仅提供一个 `test.zip`（7,932,722 bytes）。下载后 SHA-256 为 `dc928be8e95f410b0cc68abfcf33104aac72d9c9aac353ad54a39e63afbe2697`，保存在仓库外 `external-data/ucb_dataset_v2/`。
- 压缩包含 98 张 640×640 图片和 98 个 YOLO 标签，合计 2,910 个框、6 个数字类别；坐标/标签格式问题为 0。包内没有 `data.yaml` 或 `classes.txt`，不能可靠映射到项目的三类。文件名与标签显示为单一近似连续课堂机位；按 Hamming≤4 审计，本来源内部有 2,059 对近重复。
- 新增 `model_train/25-audit-ucb-dataset.py`，与 grouped train/valid/test、SCB5 和此前审计的数据源合计 28,695 张图比较。98/98 候选都匹配已有数据：89 张匹配 DataMountainSCB、9 张匹配 Class Monitoring。因此有效新增图片为 0；此 `test.zip` 保持评估专用，没有拿来训练或评分。审计报告与明细在仓库外 `external-data/ucb_dataset_v2/review/`。
- 本轮没有改动模型权重；由于来源全重复且类名缺失，不做外部 mAP 评估。当前目标课堂视频目录仍缺失，真实目标域 mAP50≥0.7 尚未验证。

## 第十九轮：智慧课堂学术数据集来源核查（2026-10-18）

- 发现 [Smart-Classroom-Student-Behavior-Dataset](https://github.com/master-weixiao/Smart-Classroom-Student-Behavior-Dataset)，README 指明视频来自国家资源公共服务平台 2019 年小学语文优课，并注明仅限学术研究。其正面视角标签含“低头写字、低头看书、举手”等目标行为，类名和框数定义较清晰；README 报告该视角有 593 个片段、574 MB。
- 通过公开分享页确认可见 `正方视角.zip`，大小 601,903,242 bytes。分享页和提取码验证成功，但公开分享列表的文件内容下载接口返回百度网盘 errno 112；当前运行环境没有完成实际压缩包传输，因此尚未看到包内文件或标签，也没有把这项来源计入数据增量、外部评分或训练。
- 若获得该压缩包，应先放在仓库外 `external-data/smart_classroom_2019/`，核验压缩完整性和视频/标签结构，再与现有 28,695 张参考图做去重，并按源视频拆分。仅保留研究用途，不分发来源视频或派生数据。
- 这是目前检索到最贴近三类目标、值得继续核验的公开来源，但数据是否有足够独立样本仍未知。目标课堂原视频目录仍不存在，真实目标域 mAP50≥0.7 尚未证实；当前正式 checkpoint 未改动。

## 第二十轮：新数据候选与类别短板复核（2026-10-18）

- 复核正式 checkpoint 在已用于开发的 SCB5 诊断集上的逐类结果（960 + TTA + NMS IoU 0.50）：举手 AP50=0.7826、阅读=0.5946、书写=0.6045。优先补足阅读/书写的真实课堂标注和统一判定规则；不再优先调举手类权重。该诊断集不是盲测。
- 核查 HCCB（Highly-Congested Classroom Behavior Dataset）候选。其官方代码仓库说明数据不随代码公开；非商业学术研究需向作者合理申请并接受数据使用协议，且限制身份识别、跟踪和学生评价等用途。当前没有获得访问批准，因此未请求、下载或训练使用该数据。
- 数据源：[HCCB 官方仓库及数据可用性说明](https://github.com/FX-CMX/ODER-HSFNet)。当前公开可直接取得的数据候选仍不足以证明目标学校/教室达到 mAP50≥0.7；最有价值的后续输入仍是有权使用的目标课堂视频，并按完整源视频划分 train/validation/test。

## 第二十一轮：Roboflow Student-Classroom-Behaviour v2 审计（2026-10-18）

- 从本机用户环境读取 `ROBOFLOW_API_KEY`（未写入日志或文件），下载 [Student-Classroom-Behaviour v2](https://universe.roboflow.com/project-yzut0/student-classroom-behaviour/dataset/2) 至仓库外 `external-data/roboflow_studentclassroombehaviour_v2/`。来源页面标注 CC BY 4.0；导出有 2,600 张图，实际 2,590 张配对图片/标签，YOLO 框格式检查问题为 0。
- Roboflow 版本 API 的类映射为空；导出 `data.yaml` 的 6 个“类名”实际是网页说明和导出时间文本，不是行为名称。按数字类统计有 15,762 个框（ID 0–5），但没有可核实的类别语义。抽样预览显示类别可能包含不同学生姿势/框类型，无法仅凭少量图片安全地把 ID 映射到 `hand-raising`、`reading`、`writing`。
- 新增 `model_train/27-audit-roboflow-student-classroom-behaviour.py` 执行全量 dHash 审计：候选 2,590 张与 33,533 张已有参考图片在阈值≤4 下无跨源命中，但来源内部存在 513,702 个近重复图对，显示帧冗余显著。预览脚本 `model_train/26-preview-roboflow-class-mapping.py`、`model_train/28-preview-roboflow-class-scenes.py` 与审计报告位于来源目录 `review/`。
- 新增 `model_train/29-analyze-roboflow-label-relations.py` 统计框粒度：不同 ID 的中位归一化面积从 0.0020 到 0.0161，相差约 8 倍；ID 0、2 的框明显窄小，ID 1、3 的框更接近完整学生框，ID 4、5 处于中间尺度，且不同 ID 常在同一图像内共现。该结构进一步表明标签可能混有不同粒度/语义，不能只按框样例猜三类行为映射；详见 `review/label_relations.json`。
- 因类别映射不可证实，即使图片新颖且边界框格式有效，也不加入训练、不做类别评估；保留数据和报告供人工向数据发布者确认类表。没有改动当前模型权重。真实目标课堂 mAP50≥0.7 仍未验证。

## 第二十二轮：SCB5 阅读标注差异人工抽样（2026-10-18）

- 核查作者账号发布的 [SCB5 Handrise/Read/Write](https://huggingface.co/datasets/wintonYF/SCB-Dataset/tree/main/SCB5-Handrise-Read-write-2024-9-17) 压缩包（779,384,408 bytes，SHA-256 `46619af0c0dea011b09b8d50f4c0578420b881b5154e9d11f0447760193a208d`），压缩完整性通过；数据许可遵循 [原作者仓库说明](https://github.com/Whiffe/SCB-dataset)，仅供学术/个人非商业研究。
- 全量 dHash 对照发现：6,864 张 train/val 图像全部与项目既有图像在汉明距离≤4 内匹配，没有可确认的新场景图像。针对精确重复的图像比较标签，候选数据相对项目标签多出大量未匹配阅读框；这只是标注差异，不能直接视作补全。
- 使用 `model_train/32-preview-scb5-label-differences.py` 对 570 张有差异的 grouped-train 精确重复图随机抽 20 张查看。额外阅读框有些落在明显低头写字、听讲或遮挡的学生区域；仅凭静态画面也无法稳定区分阅读与书写。故不把这些框合并进训练集，也不据此微调模型，避免把标签噪声带入训练。彩色差异预览保存在仓库外 `external-data/scb5_rw_hf/review/reading_label_diff_samples.jpg`，标签对照明细位于同目录 `duplicate_label_comparison.json`。
- 当前正式模型保持不变。改进优先级是取得目标课堂视频，按源视频隔离数据，再针对阅读/书写建立一致的人工标注规则和验证集；现有 SCB5 开发诊断分数不代表新课堂泛化成绩。

## 第二十三轮：960 输入 head-only 微调探针（2026-10-18）

- 从正式 checkpoint 出发，以 960 输入、AdamW 低学习率和 head-only 设置微调；第 5 轮为最佳，训练在第 10 轮早停。训练过程验证最佳 mAP50/mAP50-95 为 0.67170/0.47895。
- 使用 TTA、NMS IoU=0.50 做成对复评：

  | 评估集 / 输入 | 正式模型 mAP50 / mAP50-95 | 960 微调候选 mAP50 / mAP50-95 |
  |---|---:|---:|
  | grouped validation / 768 | 0.74070 / 0.55388 | 0.73620 / 0.55129 |
  | grouped validation / 960 | 0.73161 / 0.53682 | 0.72859 / 0.53718 |
  | SCB5 开发诊断 / 960 | 0.66057 / 0.48598 | 0.65767 / 0.48452 |

- 候选在两套数据的 mAP50 均低于正式模型，SCB5 的 mAP50-95 也降低；不保留为推荐模型。正式 checkpoint 未改变，候选权重 SHA-256 为 `86793cb741256f593f1711dbfe36927344d4ace1a767442944f55e61875153fd`，评估报告位于 `model_train/runs_grouped/validation/highres_headonly_pair_grouped_768/`、`highres_headonly_pair_grouped_960/` 和 `highres_headonly_pair_scb5_960/`。

## 第二十四轮：SCB-Dataset3-S 重复图像标签补充微调（2026-10-18）

- 审计作者发布的 SCB-Dataset3-S `5k_HRW_yolo_Dataset_jpg.zip`，来源仓库注明仅限学术/个人非商业研究；压缩包 SHA-256 为 `a72cd9513deffb1c44ea00813feae105d896fe027e809d48374e146f4ddf9cfa`。数据保存在仓库外 `external-data/scb_dataset3_s/`，不复制或分发来源图像。
- 5,015 张图像在 dHash≤4 下全部匹配项目已有图像，无独立的新场景训练样本。对精确重复图像比较标签后，仅保留满足边界合法、与现有同类框不重复且不与异类框冲突的 101 个候选框（举手 14、阅读 54、书写 33）；其余候选不加入。使用 `model_train/36-prepare-scb3s-label-enrichment.py` 生成仓库外训练副本 `external-data/scb3s_label_enriched_v2/`。
- 准备时发现 SCB5 图片位于 `images/<split>/`、标签位于平级 `labels/<split>/`；旧路径拼接曾把已有标注误判成背景。修正配对路径后复核 4,277 张训练图片、19 张背景、0 张损坏；验证集和测试集列表与正式 grouped 列表逐字节一致。正式 checkpoint 未修改。
- 从正式 head-only checkpoint 初始化，按相同 head-only AdamW 微调设置训练；第 6 轮早停，训练记录中的最佳权重来自第 1 轮。候选 SHA-256：`9384b965467a088051f40fdb9e8a57a14e583014ca1c0067f6dde1fb167747fb`；权重与训练记录保存在 `model_train/train_result_experiments/yolo11m_scb3s101_label_enrich_headonly_v2_classroom_behavior_*`。
- 固定相同评估口径做成对复评：grouped validation 为 768 + TTA + NMS IoU 0.50；SCB5 为 960 + TTA + NMS IoU 0.50。SCB5 已用于开发，不是盲测。

  | 模型 | grouped validation mAP50 / mAP50-95 | SCB5 开发诊断 mAP50 / mAP50-95 |
  |---|---:|---:|
  | 正式模型 | 0.74070 / 0.55386 | 0.66066 / 0.48620 |
  | SCB3-S 101 框补标候选 | 0.74043 / 0.55452 | 0.66040 / 0.48549 |

- 候选在两套数据上的 mAP50 都略降，grouped mAP50-95 仅增加 0.00066、SCB5 mAP50-95 略降；整体没有一致收益，不取代正式 checkpoint。SCB3-S 只提供了已有场景的标签差异，未解决新课堂域差异。成对报告（CSV、JSON）已整理到 `model_val/reports/release_20261018/`；本地原始运行目录、图像和预测可视化不纳入提交。当前真实目标课堂 mAP50≥0.7 仍未验证。

## 第 25 轮：SAV 课堂动作数据源核查（2026-10-18）

- 核查了 [SAV 官方仓库](https://github.com/Ritatanz/SAV) 与[论文](https://arxiv.org/abs/2409.00926)：论文描述 4,324 个片段、758 间教室、15 类学生动作；仓库提供动作框、person-box、帧清单和原视频链接。本轮将这些标注/清单文件下载到仓库外 `external-data/sav_dataset_audit/`，没有下载视频或图像帧。
- 类别表映射为 `read=5`、`raise_hand=8`、`take_notes=10`，与项目三类存在候选映射（阅读、举手、书写）；“take notes”仍需看原帧确认与项目 writing 定义一致。训练/验证分别有 39,466/10,745 个 read 框、5,643/1,567 个 raise_hand 框、6,550/1,955 个 take_notes 框；所查动作框越界/格式问题为 0，重复动作行分别为 25/3。
- SAV 提供的 clip ID 在 train/val 间不重复（3,459 / 865），但仓库裁剪脚本用 ID 前两段定位同一原视频。按该分组，train 有 753 个原视频组、val 有 520 个，其中 **515 个原视频组同时出现在两侧**。因此官方 val 不是源视频隔离的独立验证集；若获得授权，应先按原视频 ID 重划分，并且不能把官方 val 分数当作盲测。
- 仓库说明原始视频并非数据集作者拍摄，而是提供来自 Youku 的原视频链接；目前没有查到明确的数据/视频再利用许可证。故未下载、抽帧或训练使用原视频；本轮数据只作为来源结构和标签映射审计，不计入任何模型分数。逐项统计与文件 SHA-256 保存在仓库外 `external-data/sav_dataset_audit/metadata_audit.json`，758 个原视频分组及每组标签计数在 `source_group_split_audit.csv`。目标课堂原始素材目录仍待用户放入有权使用的视频。
