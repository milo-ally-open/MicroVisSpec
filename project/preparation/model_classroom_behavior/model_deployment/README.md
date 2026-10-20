# 课堂行为检测：Atlas 视频验收阶段 SOP

## 目的与边界

本阶段在 Atlas 200I DK A2 上构建并运行独立 AscendCL 视频 demo，检查课堂 OM、类别标签、H.264 解码、逐帧推理、检测框叠加和 H.264 编码。demo 输出 H.264 elementary stream 文件及运行统计。它不是正式常驻服务，也不负责模型安装/激活、业务告警、云边消息或 systemd；正式服务在 project/src/device_sides/atlas200idka2/。

## 准备模型、标签和视频

1. 从 model_convert 阶段生成 OM，并确认 SHA256SUMS 与实际 OM 一致。将其放在本目录，文件名为 yolo11n_classroom_behavior_best.om。
2. 检查 classes.txt 每行顺序与训练 data.yaml / 模型输出一致：

```text
hand-raising
reading
writing
```

3. 输入必须是 H.264。仓库 video/classroom_behavior.mp4 若为 HEVC，从仓库根目录在有 FFmpeg 的开发机转码，再传到板端：

```bash
ffmpeg -y -i video/classroom_behavior.mp4 -vf 'scale=trunc(iw/2)*2:trunc(ih/2)*2' -c:v libx264 -pix_fmt yuv420p -an /tmp/classroom_behavior_h264.mp4
ssh root@192.168.0.2 'mkdir -p /root/model_library/model_classroom_behavior/model_deployment'
scp /tmp/classroom_behavior_h264.mp4 root@192.168.0.2:/root/model_library/model_classroom_behavior/model_deployment/
```

也可先使用本目录 samples/smoke_h264.mp4 检查 CANN、模型和媒体链路。此短 smoke 不能说明模型在真实课堂视频上的检测精度。

## 板端构建

在 Atlas 上进入本目录，加载 CANN 环境后构建：

```bash
cd /root/model_library/model_classroom_behavior/model_deployment
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j2
```

CMake 依赖 Atlas ARM64 CANN ACL/DVPP、OpenCV C++、CMake、PkgConfig 和 FFmpeg 的 libavformat/libavcodec/libavutil 开发包。源码入口为 src/main.cpp，媒体管线和 Region OSD 头文件在 src/include/。

## 运行与检查结果

本地 H.264 文件：

```bash
./build/atlas_yolo_acl_demo --model yolo11n_classroom_behavior_best.om --video classroom_behavior_h264.mp4 --output detected.h264 --labels classes.txt --conf 0.25 --iou 0.45
```

H.264 RTSP 源：

```bash
./build/atlas_yolo_acl_demo --model yolo11n_classroom_behavior_best.om --video 'rtsp://camera-address/stream' --output detected.h264 --labels classes.txt --conf 0.25 --iou 0.45
```

程序需报告模型输出形状、视频输入/输出尺寸、处理和编码帧数、FPS、推理/后处理/OSD/编码耗时及峰值 RSS。完成后检查输出不是空文件并可解码：

```bash
ffprobe -v error -f h264 -count_frames -show_entries stream=codec_name,width,height,nb_read_frames -of default=noprint_wrappers=1 detected.h264
ffplay -f h264 -framerate 25 detected.h264
```

若模型类别数与 classes.txt 不符、OM 输入/输出类型或维度不符合预期，程序应失败；先检查 ONNX→ATC 转换和标签顺序，不要通过改标签绕过错误。

## 复现记录

归档代码提交、板卡/SOC、CANN、OpenCV、FFmpeg、CMake 版本，OM SHA-256、classes.txt SHA-256、输入视频编码/尺寸/帧数、完整命令、退出码、处理帧数和生成输出哈希。报告真实输入视频的 smoke 结果时注明授权和来源；短烟测不得写成测试集精度结论。
