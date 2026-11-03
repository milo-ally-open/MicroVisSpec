# 仓储检测：Atlas OM 转换阶段 SOP

本阶段统一包含固定参数 ONNX 导出与 Atlas ATC 转换。先按 [EXPORT.md](EXPORT.md) 导出并记录 ONNX/权重哈希，再按本文件将同一 ONNX 转换为 OM。ONNX 与 OM 产物保存在本阶段的 artifacts/ 下。

## 目的与输入

使用目标板 Atlas 200I DK A2 的 CANN ATC，把 02_model_convert 输出的固定 640×640 ONNX 转成 Ascend 310B4 OM。脚本的输入张量声明为 batch 1 NCHW，配套 aipp_nv21.cfg 将设备侧 640×640 NV21 图像送入静态 AIPP。

## 传输当前实验产物

从 WSL/Linux 仓库根目录执行；板端 SSH/SCP 会交互式询问认证信息：

```bash
ssh root@192.168.0.2 'mkdir -p /root/model_library/model_warehouse_detection/02_model_convert /root/model_library/model_warehouse_detection/02_model_convert/artifacts /root/model_library/model_warehouse_detection/04_model_demo'
scp project/preparation/model_warehouse_detection/02_model_convert/convert_atc.sh project/preparation/model_warehouse_detection/02_model_convert/aipp_nv21.cfg root@192.168.0.2:/root/model_library/model_warehouse_detection/02_model_convert/
scp project/preparation/model_warehouse_detection/02_model_convert/artifacts/yolo11n_warehouse_detection_best.onnx root@192.168.0.2:/root/model_library/model_warehouse_detection/02_model_convert/artifacts/
```

确认所传 ONNX 与 02_model_convert/artifacts/export_manifest.json 内的 SHA-256 一致。

## ATC 转换

```bash
ssh root@192.168.0.2
```

登录后在板端执行：

```bash
cd /root/model_library/model_warehouse_detection
source /usr/local/Ascend/ascend-toolkit/set_env.sh
bash 02_model_convert/convert_atc.sh 02_model_convert/artifacts/yolo11n_warehouse_detection_best.onnx
```

脚本固定 soc_version=Ascend310B4；检测不到 atc 时加载 CANN 环境，板载 Miniconda 存在时使用 base，并默认把 TE 并行编译器限制为 1。

## 结果验收和归档

输出位于 02_model_convert/artifacts/yolo11n_warehouse_detection_best/，包括 OM 和 SHA256SUMS。验证 ATC 正常结束、OM 非空、清单中的 ONNX/AIPP/OM 哈希与实际文件相符，然后把 OM 放到 04_model_demo/yolo11n_warehouse_detection_best.om。

记录板卡型号、CANN/ATC 版本、输入 ONNX 和 AIPP 哈希、OM SHA-256/大小及日志。OM 可加载性、精度差异和设备资源使用分别按 04_model_demo 与 03_model_val 阶段验收，不由转换成功推断。
