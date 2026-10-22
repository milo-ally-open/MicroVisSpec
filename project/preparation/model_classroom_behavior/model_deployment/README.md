# 课堂行为检测板端视频 Demo

本部署程序复用车辆检测研发目录中的 Atlas 200I DK A2 / AscendCL 视频推理框架；后处理依据模型输出通道数和 `classes.txt` 动态匹配类别。实际类别顺序由数据集 `data.yaml` 确定，下载数据后由 `model_train/1-download-dataset.py` 生成，不要手工猜测。

## 转换

使用 `model_convert/convert_atc.sh` 将 `model_export/artifacts/yolo11n_classroom_behavior_best.onnx` 转换为 Ascend 310B4 的 OM。脚本按输入模型文件名生成 OM 和 SHA-256 清单。

## 构建与运行

在 Atlas 200I DK A2 上：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S model_deployment -B model_deployment/build -DCMAKE_BUILD_TYPE=Release
cmake --build model_deployment/build -j2
model_deployment/build/atlas_yolo_acl_demo \
  --model model_deployment/yolo11n_classroom_behavior_best.om \
  --video <authorized-h264-classroom-video.mp4> \
  --output model_deployment/classroom-detected.h264 \
  --labels model_deployment/classes.txt --conf 0.25 --iou 0.45
```

输入需为 H.264 编码视频（可使用 MP4 容器），并来自已授权的公开样例或经授权的拍摄。演示输出只显示行为框、类别和置信度；不识别或跟踪学生身份，不把单帧行为解释成对学生的评价。

程序直接写出 Annex-B H.264 码流。需要 MP4 容器时，可按输入视频帧率用 FFmpeg 封装，例如：`ffmpeg -r 25 -i model_deployment/classroom-detected.h264 -c copy model_deployment/classroom-detected.mp4`。

终端记录的帧耗时与峰值 RSS 属于视频 Demo 运行数据，不替代 `model_val/` 在独立 test 集上的 ONNX/OM 精度、性能和内存报告。
