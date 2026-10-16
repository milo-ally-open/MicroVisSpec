# 车辆检测部署 demo

此目录是面向 Atlas 200I DK A2（Ascend 310B4）、Ubuntu 22.04 ARM64 和板端 CANN 7.0.RC1 的独立 C++/AscendCL 视频推理工程。

## 硬件链路

```text
H.264 文件或 RTSP
  → FFmpeg demux / Annex-B
  → VDEC
  → VPSS ch0（NV21 缩放与等比例留边）
  → 静态 AIPP（色彩转换、归一化）
  → ACL / Ascend 310B4 推理
  → CPU 检测框解码与 NMS
  → VPSS Region（叠加到 ch1）
  → VENC（H.264）
```

视频帧由 VDEC、VPSS 和 VENC 处理；推理输入从 VPSS 的 NV21 设备缓冲区直接送入静态 AIPP 模型。不会用 OpenCV 解码、缩放、画整帧或编码。OpenCV 仅用于检测结果 NMS、Region 的 ARGB 画布绘制。Region 绑定 VPSS 输出通道 1。

## 构建环境

依赖板端 CANN ACL 与媒体开发库、CMake、OpenCV C++ 4.x，以及 FFmpeg `libavformat`、`libavcodec`、`libavutil` 开发包。已在板端 OpenCV 4.5.4、FFmpeg 4.4、CANN 7.0.RC1 上构建。

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j2
```

头文件在 `src/include/`，由 `src/main.cpp` 引用。

## 运行

输入必须是 H.264、YUV420 视频文件或 H.264 RTSP 流；模型必须是 `model_convert/convert_atc.sh` 生成的静态 NV21 AIPP OM。输出是无容器的 H.264 elementary stream：

```bash
./build/atlas_yolo_acl_demo \
  --model yolo11n_vehicle_detection_best.om \
  --video samples/smoke_h264.mp4 \
  --output detected.h264 \
  --labels classes.txt --conf 0.25 --iou 0.45
```

RTSP 示例：

```bash
./build/atlas_yolo_acl_demo \
  --model yolo11n_vehicle_detection_best.om \
  --video 'rtsp://user:password@camera/stream' \
  --output detected.h264 --labels classes.txt
```

板端 VENC 会硬件编码 H.264。若需要 MP4 容器，用 FFmpeg 封装输出流；将 `25` 替换为程序打印的编码帧率：

```bash
ffmpeg -y -framerate 25 -i detected.h264 -c:v copy detected.mp4
```

程序输出输入/输出分辨率、处理和编码帧数、有效 FPS、NPU 推理、NMS、Region 更新、VENC 耗时分位数及峰值 RSS。若没有成功完成端到端处理的帧数，程序会报错退出。

## 输入限制

- 当前支持 H.264，不支持直接把 VP9、MPEG-4 Part 2 或 H.265 送入此 VDEC 工程。需先转为 H.264 `yuv420p`。
- 板端 VDEC 要求可解码的 H.264 coded 尺寸；遇到特殊非对齐流没有解码帧时，可把宽高缩放到 16 的倍数后再编码。仓库原始 smoke 视频有 466×466 的非标准 coded 尺寸，板端验证时将它转成了 480×480 H.264。
- VPSS ch0 输出 640×640 NV21；VPSS ch1 和 VENC 输出分辨率向上对齐到 16 像素。检测坐标会映射到编码输出分辨率。
- 推理后处理和 Region 画布绘制仍包含 CPU 工作；该 demo 优先消除整帧 CPU 解码、预处理、绘制与编码往返，并非声称 CPU 完全不参与。

车辆权重内部保存的类别编号为 `{0: 0, 1: 1, 2: 2, 3: 4}`。`classes.txt` 延续历史显示顺序（motorcycle、car、bus、truck）；语义映射来自已有报告，复测数据集时仍需与该数据集的 `data.yaml` 核对。

## 板端烟测（2026-10-16）

使用随包 `samples/smoke_h264.mp4`（3 秒、480×480、10 FPS）运行 30 帧。模型输出张量 `[1,8,8400]`，全部帧完整经过推理和编码；端到端 1.706 秒、17.588 FPS，推理均值 10.049 ms/帧，NMS 0.245 ms，Region 更新 0.380 ms，硬件 VENC 0.929 ms，峰值 RSS 364.508 MiB。生成的 H.264 经 FFmpeg 封装并由 FFprobe 检查为 480×480、10 FPS、30 帧，完整解码成功。

官方 API 参考：[VDEC 与 VPSS 视频处理流程](https://www.hiascend.com/document/detail/zh/Atlas200IDKA2DeveloperKit/23.0.RC2/Application%20Development%20Guide/aadgc/aclcppdevg_000033.html)、[Region 管理接口](https://www.hiascend.com/document/detail/zh/Atlas200IDKA2DeveloperKit/23.0.RC2/Application%20Development%20Guide/aadgc/aclcppdevg_03_0389.html)。

## 工作流来源

训练、ONNX 导出和 ATC 转换流程位于 `project/preparation/model_vehicle_detection/`。部署模型须由同一目录的 `model_convert/convert_atc.sh` 和板端配套 ATC 生成；精度报告请以 `model_val/` 对同一固定 test 集的 ONNX 与 OM 评估为准，视频 demo 烟测不是测试集精度评估。
