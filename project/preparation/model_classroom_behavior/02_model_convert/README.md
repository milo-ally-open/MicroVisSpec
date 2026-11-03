# 课堂行为检测：Atlas OM 转换阶段 SOP

本阶段统一包含固定参数 ONNX 导出与 Atlas ATC 转换。先按 [EXPORT.md](EXPORT.md) 导出并记录 ONNX/权重哈希，再按本文件将同一 ONNX 转换为 OM。ONNX 与 OM 产物保存在本阶段的 artifacts/ 下。

## 目的与输入

在 Atlas 200I DK A2 / Ascend 310B4 上，用 CANN ATC 将上一阶段的静态 ONNX 转为 OM。仓库脚本固定静态输入 1×3×640×640，并使用 aipp_nv21.cfg 配置 NV21 AIPP；部署 demo/runtime 侧输入为 640×640 NV21。转换输出依赖目标板 CANN 版本，必须记录转换设备和 toolkit 信息。

## 准备板端目录

从 WSL/Linux 仓库根目录上传转换配置和 ONNX。以下操作不传数据集或凭据：

```bash
ssh root@192.168.0.2 'mkdir -p /root/model_library/model_classroom_behavior/02_model_convert /root/model_library/model_classroom_behavior/02_model_convert/artifacts /root/model_library/model_classroom_behavior/04_model_demo'
scp project/preparation/model_classroom_behavior/02_model_convert/convert_atc.sh project/preparation/model_classroom_behavior/02_model_convert/aipp_nv21.cfg root@192.168.0.2:/root/model_library/model_classroom_behavior/02_model_convert/
scp project/preparation/model_classroom_behavior/02_model_convert/artifacts/yolo11n_classroom_behavior_best.onnx root@192.168.0.2:/root/model_library/model_classroom_behavior/02_model_convert/artifacts/
```

以上输入必须来自当前实验的 export_manifest.json 所记录的权重和 ONNX 哈希。

## 板端转换

SSH 登录 Atlas 后，在板端模型目录执行：

```bash
ssh root@192.168.0.2
```

```bash
cd /root/model_library/model_classroom_behavior
source /usr/local/Ascend/ascend-toolkit/set_env.sh
bash 02_model_convert/convert_atc.sh 02_model_convert/artifacts/yolo11n_classroom_behavior_best.onnx
```

脚本在首次找不到 atc 时加载 CANN 环境；如果检测到板载 Miniconda，会激活 base 并重新加载 CANN。target soc 固定为 Ascend310B4，编译并行度默认为 1，可通过 TE_PARALLEL_COMPILER 覆盖。

## 产物检查与入库

预期产物：02_model_convert/artifacts/yolo11n_classroom_behavior_best/yolo11n_classroom_behavior_best.om 和 SHA256SUMS。清单包含 ONNX、AIPP 配置与 OM 的 SHA-256。确认 ATC 成功、OM 非空且哈希可复算后，将 OM 复制到 04_model_demo/yolo11n_classroom_behavior_best.om；不要手工修改模型文件名或清单哈希。

记录板卡型号、Ascend 310B4、CANN/ATC 版本、执行命令、输入 ONNX/AIPP 哈希、输出 OM 哈希和文件大小。精度等价、内存和吞吐需按 03_model_val SOP 独立测量；OM 生成成功不代表其指标满足要求。
