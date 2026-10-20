# 仓储检测：Atlas OM 转换阶段 SOP

## 目的与输入

使用目标板 Atlas 200I DK A2 的 CANN ATC，把 model_export 输出的固定 640×640 ONNX 转成 Ascend 310B4 OM。脚本的输入张量声明为 batch 1 NCHW，配套 aipp_nv21.cfg 将设备侧 640×640 NV21 图像送入静态 AIPP。

## 传输当前实验产物

从 WSL/Linux 仓库根目录执行；板端 SSH/SCP 会交互式询问认证信息：

```bash
BOARD=root@192.168.0.2
ssh "$BOARD" 'mkdir -p /root/model_library/model_warehouse_detection/model_convert /root/model_library/model_warehouse_detection/model_export/artifacts /root/model_library/model_warehouse_detection/model_deployment'
scp project/preparation/model_warehouse_detection/model_convert/convert_atc.sh project/preparation/model_warehouse_detection/model_convert/aipp_nv21.cfg "$BOARD:/root/model_library/model_warehouse_detection/model_convert/"
scp project/preparation/model_warehouse_detection/model_export/artifacts/yolo11n_warehouse_detection_best.onnx "$BOARD:/root/model_library/model_warehouse_detection/model_export/artifacts/"
```

确认所传 ONNX 与 model_export/artifacts/export_manifest.json 内的 SHA-256 一致。

## ATC 转换

```bash
ssh root@192.168.0.2
```

登录后在板端执行：

```bash
cd /root/model_library/model_warehouse_detection
source /usr/local/Ascend/ascend-toolkit/set_env.sh
bash model_convert/convert_atc.sh model_export/artifacts/yolo11n_warehouse_detection_best.onnx
```

脚本固定 soc_version=Ascend310B4；检测不到 atc 时加载 CANN 环境，板载 Miniconda 存在时使用 base，并默认把 TE 并行编译器限制为 1。

## 结果验收和归档

输出位于 model_convert/artifacts/yolo11n_warehouse_detection_best/，包括 OM 和 SHA256SUMS。验证 ATC 正常结束、OM 非空、清单中的 ONNX/AIPP/OM 哈希与实际文件相符，然后把 OM 放到 model_deployment/yolo11n_warehouse_detection_best.om。

记录板卡型号、CANN/ATC 版本、输入 ONNX 和 AIPP 哈希、OM SHA-256/大小及日志。OM 可加载性、精度差异和设备资源使用分别按 model_deployment 与 model_val 阶段验收，不由转换成功推断。
