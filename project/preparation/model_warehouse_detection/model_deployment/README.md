# 仓储物品检测：Atlas 200I DK A2 部署包

此目录是仓储物品检测的完整 C++/AscendCL 视频推理 demo，面向 Ubuntu 22.04 ARM64、Atlas 200I DK A2（Ascend 310B4）。它逐帧解码视频、执行推理并写出带检测框的视频；默认输入尺寸 `640×640`，RGB、`1/255` 归一化和 114 灰边 letterbox，后处理按类别执行 NMS。

## 构建

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j2
```

构建依赖 CANN ACL 开发文件、OpenCV C++ 4.x（此板已验证 Ubuntu Jammy 的 4.5.4）和 CMake。使用 CANN/驱动版本须与设备镜像配套。

## 推理

```bash
./build/atlas_yolo_acl_demo --model yolo11n_warehouse_detection_best.om --video samples/smoke.mp4 --output samples/detected.mp4 --labels classes.txt --conf 0.25 --iou 0.45
```

程序预热 5 帧后处理完整视频并生成 `detected.mp4`，最后输出处理帧数、预处理/推理/后处理耗时分位统计和进程峰值 RSS。NPU 使用率/显存请另用 `npu-smi info` 采样。test 集精度须在 Python/Ultralytics 环境对同一数据集另行评估；视频 demo 不等同于精度评估。

当前预期模型输出为 batch=1 的 YOLO 检测张量，类别数必须与 `classes.txt` 一致。转换后程序会读取并校验输出形状；若 ATC 输出布局不符会直接报错，不会静默输出伪结果。

## 产物来源

对应流程位于仓库中的 `project/preparation/model_warehouse_detection/`。OM 由同目录 `model_convert/` 下的流程使用目标板配套 ATC 生成；运行评估前需记录 OM 与 ONNX SHA-256、CANN/驱动、数据集版本和运行参数。

## Atlas 板端公开视频实跑（2026-10-16）

使用 `samples/forklift_open_video.webm`（原始 1280×720、VP9，67.101 秒）运行本 demo；输入未经图片合成或剪辑。AscendCL 模型输出形状为 `[1,16,8400]`，处理 4,020 帧，平均推理 16.150 ms/帧、p50 15.894 ms、p95 16.368 ms，预处理平均 10.265 ms、后处理平均 64.156 ms，峰值 RSS 494.293 MiB。输出 `/tmp/warehouse-open-video.mp4` 已由 ffprobe 确认为 MPEG-4、1280×720、4,020 帧、67.068 秒。该视频运行仅验证视频解码、推理与写出链路，不代表测试集精度。
