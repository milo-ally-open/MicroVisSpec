# 车辆检测工作流

本目录只负责车辆检测。训练脚本、权重、测试报告、ONNX 转换和视频部署代码都在本模型目录内；`model_train/train_result/` 保存本模型权重与报告。数据集下载需要 Roboflow 凭据，数据集本身不提交到 Git。

## 环境

开发机沿用 WSL 中已有的 Conda `yunshao` 环境，不创建新环境：

```bash
conda env update -n yunshao -f environment.yml
conda activate yunshao
```

各阶段的 Python 与板端 C/C++ 依赖及版本见 `environment.yml`。当前提交已有训练好的 `model_train/train_result/yolo11n_vehicle_detection_best.pt`，本次复现导出和部署不需要重新训练。

## 工作流

以下命令均在本目录运行。需要重新取得数据或重新训练时再执行第 1、2 步；凭据仅通过环境变量提供。

### 1. 下载固定数据集版本

```bash
export ROBOFLOW_API_KEY='你的 Roboflow API key'
python model_train/1-download-dataset.py
```

脚本固定下载 `jey-mtuyv/vehicle-detection-tqbaf-7cck5` v1。

### 2. 数据分析与训练（按需）

```bash
python model_train/2-analyse-dataset.py
python model_train/3-train-models.py --datasets vehicle_detection --epochs 100 --imgsz 640 --batch 16 --device 0 --workers 8 --seed 42 --deterministic --name yolo11n
```

训练过程保存在 `model_train/runs/train/yolo11n_vehicle_detection/`。若重新训练，将训练输出复制到固定的权重路径：

```bash
cp model_train/runs/train/yolo11n_vehicle_detection/weights/best.pt model_train/train_result/yolo11n_vehicle_detection_best.pt
```

再运行后续步骤。不得把本次已有的权重误当成重新训练产物。

### 3. 测试集评估（数据集可用时）

```bash
python model_train/4-validate-model.py --datasets vehicle_detection --split test --checkpoint best --imgsz 640 --batch 16 --device 0
```

评估运行和 JSON/CSV 指标写在 `model_train/runs/validation/` 下。`train_result/README.md` 中已有指标是历史结果；只有固定数据集下载成功并重新执行此步骤后，才可记录为新评估结果。

### 4. 从现有权重导出 ONNX

```bash
python model_export/export_onnx.py --datasets vehicle_detection --checkpoint best --imgsz 640 --device cpu --opset 17
```

ONNX 与权重/模型哈希清单写入 `model_export/artifacts/`。

### 5. 使用板端 ATC 转换

导出产物在 `model_export/artifacts/`。板端目录按 `model_vehicle_detection/model_deployment` 和 `model_val` 组织；从 Windows PowerShell 仓库根目录上传部署包、验证脚本、ATC 脚本和 ONNX：

```powershell
scp -r .\project\preparation\model_vehicle_detection\model_deployment root@192.168.0.2:/root/model_library/model_vehicle_detection/
scp -r .\project\preparation\model_vehicle_detection\model_val root@192.168.0.2:/root/model_library/model_vehicle_detection/
scp .\project\preparation\model_vehicle_detection\model_convert\convert_atc.sh root@192.168.0.2:/tmp/convert_atc.sh
scp .\project\preparation\model_vehicle_detection\model_export\artifacts\yolo11n_vehicle_detection_best.onnx root@192.168.0.2:/tmp/yolo11n_vehicle_detection_best.onnx
```

在板端使用镜像自带 CANN/ATC，Miniconda `base` 仅用于检查其 Python 依赖，不调用系统 Python：

```bash
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cd /tmp
bash convert_atc.sh /tmp/yolo11n_vehicle_detection_best.onnx
install -D artifacts/yolo11n_vehicle_detection_best/yolo11n_vehicle_detection_best.om /root/model_library/model_vehicle_detection/model_deployment/yolo11n_vehicle_detection_best.om
sha256sum /tmp/yolo11n_vehicle_detection_best.onnx /root/model_library/model_vehicle_detection/model_deployment/yolo11n_vehicle_detection_best.om
```

ATC 脚本在板端 `/tmp/artifacts/yolo11n_vehicle_detection_best/` 保存模型和 SHA-256 清单。

### 6. 编译并运行视频 demo

把 OM 放在本目录的 `model_deployment/`，然后在开发板运行：

```bash
cd model_deployment
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j2
./build/atlas_yolo_acl_demo --model yolo11n_vehicle_detection_best.om --video samples/smoke.mp4 --output samples/detected.mp4 --labels classes.txt --conf 0.25 --iou 0.45
./build/atlas_yolo_acl_demo --model yolo11n_vehicle_detection_best.om --video samples/road_traffic_open_video.webm --output /tmp/vehicle-open-video.mp4 --labels classes.txt --conf 0.25 --iou 0.45
```

demo 接受视频输入并写出带检测框的视频；帧耗时、预处理/推理/后处理时间与峰值 RSS 会输出到终端。该 smoke test 证明部署链路可运行，不替代测试集精度评估。车辆类别的显示顺序仍应在恢复数据集后与其 `data.yaml` 核实。

### 7. ONNX/OM 精度、性能和内存对比

按 `model_val/README.md` 分别运行 `accuracy_eval.py`、`performance_eval.py`、`memory_eval.py`，生成独立报告；将板端 `om_report.json` 复制回开发机后，用 `precision_eval.py --onnx-report <onnx报告路径> --om-report <om报告路径> --output <输出前缀>` 完成总体与逐类别精度验证。四个评估脚本均使用具名 `--参数名 值`，不得把视频 smoke test 当作测试集精度报告。
