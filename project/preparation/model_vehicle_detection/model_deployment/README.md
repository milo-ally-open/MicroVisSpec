# 车辆检测：Atlas 视频验收阶段 SOP

## 目的与边界

此阶段在 Atlas 200I DK A2 / Ascend 310B4 上运行独立 C++/AscendCL 检测 demo，验收 OM 加载、类别解码、视频输入、Region OSD 和 H.264 输出。输出为不带容器的 H.264 elementary stream。视频 smoke 验证链路可运行，不替代 model_val 的 test split 指标。

## 输入和类别检查

OM 由 model_convert 阶段生成，放在本目录并命名 yolo11n_vehicle_detection_best.om。classes.txt 当前顺序为 motorcycle、car、bus、truck；必须与本次模型训练 data.yaml 和模型类别索引映射核对一致。manifest/哈希不匹配或类别数不符时停止验收。

本 demo 的 VDEC 只接受 H.264。samples/smoke_h264.mp4 可用于本地链路烟测。公开样例 samples/road_traffic_open_video.webm 是原始 WebM，不能直接送入 demo；需先转 H.264：

```bash
ffmpeg -y -i samples/road_traffic_open_video.webm -vf 'scale=640:640:force_original_aspect_ratio=decrease,pad=640:640:(ow-iw)/2:(oh-ih)/2' -c:v libx264 -pix_fmt yuv420p -an /tmp/vehicle-open-h264.mp4
```

## 板端构建与运行

在 Atlas 上从本目录构建：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j2
```

运行随包 H.264 smoke：

```bash
./build/atlas_yolo_acl_demo --model yolo11n_vehicle_detection_best.om --video samples/smoke_h264.mp4 --output /tmp/vehicle-smoke.h264 --labels classes.txt --conf 0.25 --iou 0.45
```

运行转码后的公开视频：

```bash
./build/atlas_yolo_acl_demo --model yolo11n_vehicle_detection_best.om --video /tmp/vehicle-open-h264.mp4 --output /tmp/vehicle-open-output.h264 --labels classes.txt --conf 0.25 --iou 0.45
```

程序应报告模型输出 shape、输入/输出分辨率、完整处理与编码帧数、FPS、推理/NMS/Region/VENC 耗时及峰值 RSS。检查输出可解码并查看画面：

```bash
ffprobe -v error -f h264 -count_frames -show_entries stream=codec_name,width,height,nb_read_frames -of default=noprint_wrappers=1 /tmp/vehicle-smoke.h264
ffplay -f h264 -framerate 25 /tmp/vehicle-smoke.h264
```

如果输入编码不是 H.264、解码没有输出帧或帧尺寸触发 VDEC 限制，先转码为 yuv420p H.264 并把尺寸设为 16 的倍数。不要把 WebM/H.265 原文件直接作为通过证据。

## 依赖与实验记录

CMake 依赖 CANN ACL/DVPP、OpenCV C++、FFmpeg libavformat/libavcodec/libavutil、PkgConfig 和 CMake。记录代码提交、板卡/SOC/CANN、系统依赖版本、OM 与类别文件哈希、输入视频来源/编码/尺寸/帧数、命令、退出码、处理帧数和输出 SHA-256。检测类别语义仍需用同版本 data.yaml 核验。
