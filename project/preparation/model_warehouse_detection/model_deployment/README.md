# 仓储检测：Atlas 视频验收阶段 SOP

## 目的与边界

在 Atlas 200I DK A2 / Ascend 310B4 使用 C++/AscendCL demo 验证仓储检测 OM 和视频媒体管线。demo 覆盖 VDEC、VPSS、静态 AIPP、ACL 推理、检测框/Region OSD 和 VENC H.264 输出，结果为无容器的 H.264 elementary stream。此验收不是 test split 精度报告。

## 输入核对

使用 model_convert 阶段输出的 yolo11n_warehouse_detection_best.om，放置于本目录。classes.txt 顺序为 Toolbox、bottles、extinguisher、fitting box、forklift、full box、pallet、pallet_truck、palletjack、person、small_load_carrier、stillage；执行前须和当前 data.yaml/模型输出索引核对。模型类别数或标签不一致时停止运行。

仅支持 H.264 输入。samples/smoke_h264.mp4 用于初始 smoke；公开文件 samples/forklift_open_video.webm 需要转码后才可使用：

```bash
ffmpeg -y -i samples/forklift_open_video.webm -vf 'scale=640:640:force_original_aspect_ratio=decrease,pad=640:640:(ow-iw)/2:(oh-ih)/2' -c:v libx264 -pix_fmt yuv420p -an /tmp/warehouse-open-h264.mp4
```

## 板端构建及运行

加载板端 CANN 后从本目录构建：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j2
```

先运行随包短视频：

```bash
./build/atlas_yolo_acl_demo --model yolo11n_warehouse_detection_best.om --video samples/smoke_h264.mp4 --output /tmp/warehouse-smoke.h264 --labels classes.txt --conf 0.25 --iou 0.45
```

再运行转码后的公开样例：

```bash
./build/atlas_yolo_acl_demo --model yolo11n_warehouse_detection_best.om --video /tmp/warehouse-open-h264.mp4 --output /tmp/warehouse-open-output.h264 --labels classes.txt --conf 0.25 --iou 0.45
```

终端应显示 OM 输出维度、输入/输出尺寸、处理帧数、FPS、推理/NMS/Region/VENC 延迟和峰值 RSS。检查输出：

```bash
ffprobe -v error -f h264 -count_frames -show_entries stream=codec_name,width,height,nb_read_frames -of default=noprint_wrappers=1 /tmp/warehouse-smoke.h264
ffplay -f h264 -framerate 25 /tmp/warehouse-smoke.h264
```

Atlas VDEC 只接受 H.264；如解码失败，检查码流并使用 yuv420p 重新编码。输入尺寸异常时将输出设为 16 的倍数后重试。WebM 源文件不能作为通过记录。

## 追溯记录

记录提交号、板卡/SOC/CANN、CMake/OpenCV/FFmpeg 版本、OM/classes SHA-256、输入视频来源和编码、帧数、完整命令、退出码、输出帧数及 H.264 文件哈希。demo smoke 只证明硬件推理和编码链路工作；精度、性能对比仍以 model_val 的同源测试报告为准。
