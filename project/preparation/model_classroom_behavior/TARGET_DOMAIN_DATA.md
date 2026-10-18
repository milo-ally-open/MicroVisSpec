# 目标课堂数据采集与标注

当前模型在 grouped validation 上的 mAP50 为 0.7406；在已用于开发的 SCB5 诊断集上，较优推理配置为 960 + TTA，mAP50 为 0.6606。新来源 DataMountainSCB 的一次性来源测试 mAP50 为 0.3000。公开图像集未提供足够独立增量，下一轮需要真实目标课堂素材。

## 素材入口

把有权使用的视频放到仓库外的数据目录。以下用 `CLASSROOM_DATA_ROOT` 表示该目录；运行命令前按本机位置设置，例如：

```powershell
$env:CLASSROOM_DATA_ROOT = Read-Host '输入仓库外的数据目录路径'
```

建议提供多个完整课段或不同摄像头视角，保留原分辨率，不要只截取模型容易识别的片段。文件名可以使用匿名编号，不需要包含学校、班级或学生姓名。视频文件不会放入 Git 仓库。

## 抽帧

先切换到 `model_classroom_behavior` 目录，再在项目虚拟环境中运行：

```powershell
.venv\Scripts\python.exe `
  model_train\19-prepare-target-classroom-videos.py `
  --input-dir (Join-Path $env:CLASSROOM_DATA_ROOT 'target_videos') `
  --output-dir (Join-Path $env:CLASSROOM_DATA_ROOT 'target_frames') `
  --interval-seconds 2
```

输出包含 JPEG 帧、`frames_manifest.csv` 和 `sampling_summary.json`。清单为每帧保留源视频 SHA-256、视频 ID、帧号和时间戳。抽帧脚本不做预测、不自动标注，也不分配数据集 split。

## 标注规则

- 每个可判断的学生行为单独画框，沿用项目的三个类：`hand-raising`、`reading`、`writing`。
- `reading`：画面能支持学生正在关注文本或阅读材料；仅低头或桌上有书不足以判定。
- `writing`：画面能支持学生正在纸面或书写面上产出文字；只握笔、低头或手被遮挡时无法确定的样例标为歧义，先不强行归类。
- `hand-raising`：学生的手臂明显举起参与课堂；只看到桌面以上的手或姿势不清楚时，单独标为歧义待复核。
- 同一学生可以在相邻时间点改变类别；不要根据相邻帧推断当前帧中看不见的动作。

## 防止数据泄漏

- split 的单位是源视频。一个视频抽出的所有帧只能进入 train、validation 或 test 其中一个 split。
- 至少留出完整视频作为 test；在选模型、调置信度或改标注定义时都不要查看 test 指标。最终只评估一次。
- 类别覆盖不足时，优先补采对应动作的视频，而不是从同一短片复制更多相邻帧。
- 完成标注后先检查近重复、缺失标签、越界框和类别数量，再合并训练。保留视频级 split 清单和每次标签修订记录。

## 交付结构

```text
target_classroom_dataset/
  train/images/  train/labels/
  valid/images/  valid/labels/
  test/images/   test/labels/
  data.yaml
  source_video_splits.csv
  annotation_changes.csv
```

## 标注完成后的数据打包

将每帧 YOLO 标注保存为与帧文件同名的 `.txt`，放到单独的平铺目录；没有目标行为的帧也要放一个空 `.txt`，表示该帧已人工检查。标注编号固定为 `0=hand-raising`、`1=reading`、`2=writing`。

先人工创建 `source_video_splits.csv`，每个 `source_video_id` 只能出现一次：

```csv
source_video_id,split
<视频ID A>,train
<视频ID B>,valid
<视频ID C>,test
```

同一源视频的全部抽帧必须在同一个 split。完成标注和视频级划分后，在项目虚拟环境运行审计和打包脚本：

```powershell
.venv\Scripts\python.exe `
  model_train\37-prepare-target-classroom-dataset.py `
  --frames-dir (Join-Path $env:CLASSROOM_DATA_ROOT 'target_frames') `
  --labels-dir (Join-Path $env:CLASSROOM_DATA_ROOT 'target_annotations') `
  --source-splits (Join-Path $env:CLASSROOM_DATA_ROOT 'source_video_splits.csv') `
  --output-dir (Join-Path $env:CLASSROOM_DATA_ROOT 'target_classroom_dataset')
```

脚本会检查缺少/多余标签、类别编号、越界框、视频级 split 泄漏和跨 split 完全重复帧，并生成 YOLO 数据目录、`data.yaml` 与 `preparation_report.json`。输出目录必须为空或不存在；修订标注后请使用新的输出目录，保留每轮审计记录。
