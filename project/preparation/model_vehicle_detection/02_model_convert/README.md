# 车辆检测：Atlas OM 转换阶段 SOP

本阶段统一包含固定参数 ONNX 导出与 Atlas ATC 转换。先按 [EXPORT.md](EXPORT.md) 导出并记录 ONNX/权重哈希，再按本文件将同一 ONNX 转换为 OM。ONNX 与 OM 产物保存在本阶段的 artifacts/ 下。

## 目的与固定转换

把 02_model_convert 阶段的静态 640×640 ONNX 转为 Atlas 200I DK A2 / Ascend 310B4 OM。convert_atc.sh 使用静态 batch 1、NCHW 输入定义和 aipp_nv21.cfg 的 NV21 静态 AIPP；需在目标板上的 CANN ATC 环境执行，不能用桌面工具生成的 OM 替代。

## 上传必要文件

从 WSL/Linux 仓库根目录运行。只上传转换脚本、AIPP 配置和当前 ONNX：

```bash
ssh root@192.168.0.2 'mkdir -p /root/model_library/model_vehicle_detection/02_model_convert /root/model_library/model_vehicle_detection/02_model_convert/artifacts /root/model_library/model_vehicle_detection/04_model_demo'
scp project/preparation/model_vehicle_detection/02_model_convert/convert_atc.sh project/preparation/model_vehicle_detection/02_model_convert/aipp_nv21.cfg root@192.168.0.2:/root/model_library/model_vehicle_detection/02_model_convert/
scp project/preparation/model_vehicle_detection/02_model_convert/artifacts/yolo11n_vehicle_detection_best.onnx root@192.168.0.2:/root/model_library/model_vehicle_detection/02_model_convert/artifacts/
```

先核对 export_manifest.json 中的 ONNX 文件名和 SHA-256，确保这里上传的是本次导出产物。

## 板端执行

```bash
ssh root@192.168.0.2
```

登录后在板端执行：

```bash
cd /root/model_library/model_vehicle_detection
source /usr/local/Ascend/ascend-toolkit/set_env.sh
bash 02_model_convert/convert_atc.sh 02_model_convert/artifacts/yolo11n_vehicle_detection_best.onnx
```

ATC soc 固定 Ascend310B4；脚本会在需要时 source CANN，并在存在板载 Miniconda 时使用 base 环境，编译器并发默认限制为 1。

## 输出与追溯

OM 和 SHA256SUMS 写入 02_model_convert/artifacts/yolo11n_vehicle_detection_best/。清单哈希覆盖输入 ONNX、AIPP 配置和输出 OM。确认文件非空、`sha256sum -c SHA256SUMS` 成功，然后复制 OM 到 04_model_demo/yolo11n_vehicle_detection_best.om 供 demo 和 OM 评估使用。

实验记录需保留板卡/SOC、CANN/ATC 版本、输入文件哈希、命令、日志、输出 OM 哈希与大小。编译通过仅说明格式/转换完成，不能当作模型精度或部署性能结论。
