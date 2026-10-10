# 消防通道静止目标检测阶段化说明

> 状态：算法路线草案；训练数据与场景适配、通道区域判定方法、RK3588 实机条件待团队确认
> 责任边界：队友负责图片/算法及边缘推理；平台事件接入由双方约定
> 注意：本文件描述的阶段和技术方案不是当前前后端模拟联调版的已批准交付范围。


## 稳定需求与验收索引

> 本索引用于跨文档追踪；详细行为以本文件正文为准。编号保持稳定，需求变更时同步更新。

### REQ-01 数据集与模型候选

保留两类场景的模型候选与可追溯评估，数据来源待确认。

- **AC-01.1**：评估分别覆盖楼道杂物和消防车通道车辆及选定指标。
- **AC-01.2**：数据版本、许可、类别映射及权重核验后才能冻结基线。

### REQ-02 端侧推理

区分桌面实验与 RK3588 实机结果并可复现。

- **AC-02.1**：实机记录转换、精度、时延、内存和稳定性。
- **AC-02.2**：桌面或模拟结果不得标为 RK3588/NPU 实测。

### REQ-03 静止判定

轨迹稳定性、静止时间及区域规则仍待验证。

- **AC-03.1**：跟踪状态和速度平滑覆盖丢失、抖动及非法值。
- **AC-03.2**：阈值、持续时间和区域规则经评估后确定。

### REQ-04 C/C++ 媒体链路

探索本地文件及 RTSP 端侧推理，待实机联调。

- **AC-04.1**：本地文件推理可重复并输出结构化结果。
- **AC-04.2**：RTSP 恢复、资源释放和部署须目标板实测。

## 一、任务目标

针对两类消防通道场景，完成目标检测、静止目标判断、RK3588 端侧验证及 C/C\+\+ 最小工程部署。

整体划分为三个阶段：

1. **第一阶段：模型训练及端侧验证**
2. **第二阶段：静止目标后处理算法**
3. **第三阶段：算法迁移及 C/C\+\+ 最小 Demo**

最终处理链路为：

**媒体输入 → YOLO11n → ByteTrack → 归一化视觉速度 → EMA → 静止计时 → 报警**

---

## 二、第一阶段：YOLO 模型训练及端侧验证

### 2\.1 两个垂直检测模型

| 模型 | 场景 | 数据集 | 主要类别 | 预训练权重 |
|---|---|---|---|---|
| 模型 A | 楼道消防通道杂物检测 | [Roboflow Warehouse Detection](https://universe.roboflow.com/warehouse-recognition/warehouse-detection-xtbsn) | pallet、fitting box、full box、pallet_truck、palletjack、small_load_carrier、stillage、Toolbox | **YOLO11n** |
| 模型 B | 消防车通道车辆检测 | [Roboflow Vehicle Detection](https://universe.roboflow.com/jey-mtuyv/vehicle-detection-tqbaf-7cck5) | car、truck、bus、motorcycle | **YOLO11n** |

YOLO11 参考：

[Ultralytics YOLO11 Documentation](https://docs.ultralytics.com/models/yolo11/)

模型统一采用 **YOLO11n**。

若 Nano 模型精度不足，可进一步测试 YOLO11s，但默认以 YOLO11n 作为训练和部署基线。

---

## 三、模型 A：楼道杂物检测

从 Hazard Detection 数据集中筛选与消防通道堵塞直接相关的类别。

|类别                |含义      |
|------------------|--------|
|cardboard-box     |纸箱      |
|blocking-object   |阻挡物     |
|debris-on-floor   |地面杂物    |
|cleaning-equipment|清洁设备、推车等|

YOLO 模型只负责：

- 目标分类
- Bounding Box 定位
- 置信度输出

不直接判断是否形成消防通道堵塞。

建议训练参数：

|参数        |建议值        |
|----------|----------:|
|模型        |YOLO11n    |
|输入尺寸      |640 × 640  |
|Epochs    |100        |
|Batch Size|根据 GPU 显存确定|
|初始化       |COCO 预训练权重 |

---

## 四、模型 B：消防车通道车辆检测

检测类别：

|类别       |含义 |
|---------|---|
|car      |小汽车|
|truck    |货车 |
|bus      |客车 |
|motorbike|摩托车|

后处理阶段可统一视为 vehicle。

训练参数与楼道模型保持一致。

---

## 五、模型训练评估

PC 训练环境下首先完成标准检测性能评估。

重点指标：

|指标          |目的      |
|------------|--------|
|Precision   |衡量误检情况  |
|Recall      |衡量漏检情况  |
|mAP@0.5     |基础检测能力  |
|mAP@0.5:0.95|综合检测定位能力|
|FPS         |PC 基准性能 |
|模型文件大小      |部署资源评估  |

消防安全场景重点关注 Recall，降低目标漏检。

---

## 六、RK3588 模型端侧验证

模型训练完成后，在 RK3588 上完成完整的模型端侧验证。

Rockchip 相关工具：

- [RKNN\-Toolkit2](https://github.com/airockchip/rknn-toolkit2)
- [RKNN Model Zoo](https://github.com/airockchip/rknn_model_zoo)
- [RKNN Model Zoo YOLO11 示例](https://github.com/airockchip/rknn_model_zoo/tree/main/examples/yolo11)

基本模型链路：

**YOLO11n PyTorch → ONNX → RKNN → RK3588 NPU**

### 6\.1 模型精度分析

比较：

|模型               |运行环境        |
|-----------------|------------|
|YOLO11n PyTorch  |PC          |
|YOLO11n ONNX     |PC          |
|YOLO11n RKNN     |RK3588      |
|YOLO11n RKNN INT8|RK3588，如采用量化|

统计：

- Precision
- Recall
- mAP@0\.5
- mAP@0\.5:0\.95
- 置信度变化
- 典型误检、漏检
- 量化精度损失

最终形成 PC 模型与 RK3588 模型精度对比结果。

### 6\.2 模型性能评估

在 RK3588 实机统计：

|指标         |内容          |
|-----------|------------|
|单帧 NPU 推理时间|网络前向耗时      |
|前处理时间      |Resize、颜色转换等|
|后处理时间      |输出解析、NMS 等  |
|单帧总时间      |一次完整目标检测    |
|FPS        |单路实际处理能力    |

### 6\.3 模型内存评估

统计：

|指标           |内容        |
|-------------|----------|
|RKNN 文件大小    |模型磁盘空间    |
|Runtime 初始化内存|加载运行环境后的占用|
|模型加载内存       |模型加载后的内存增量|
|推理峰值内存       |连续推理过程峰值  |
|总进程峰值        |完整测试进程内存  |

第一阶段的 RK3588 精度、性能和内存测试统一称为：

> **模型端侧验证**

### 6\.4 第一阶段交付物

包括：

- 楼道杂物 YOLO11n 模型
- 消防车通道车辆 YOLO11n 模型
- ONNX 模型
- RKNN 模型
- 模型训练评估结果
- RK3588 精度分析
- RK3588 性能评估
- RK3588 内存评估
- 典型推理结果

---

## 七、第二阶段：静止目标后处理算法

后处理只关注：

> **同一个检测目标是否持续保持静止。**

统一流程：

**YOLO → ByteTrack → Track ID → 归一化视觉速度 → EMA → MOVING / STATIONARY → 静止计时 → ALARM**

不使用：

- 人工 ROI
- 尺寸判断
- MOG2
- 背景建模
- 真实物理速度

---

## 八、ByteTrack 目标跟踪

YOLO 负责逐帧检测，ByteTrack 负责跨帧关联目标。

每个目标获得独立 Track ID。

每个 Track 维护：

|信息      |用途                         |
|--------|---------------------------|
|Track ID|唯一目标标识                     |
|上一时刻中心点 |计算视觉运动量                    |
|EMA 平滑速度|判断运动状态                     |
|静止时间    |判断报警                       |
|当前状态    |MOVING / STATIONARY / ALARM|

---

## 九、归一化视觉速度

根据 Bounding Box 中心点计算相邻时刻位移：

&#91;
D\_t=
\\sqrt\{&#40;x\_t\-x\_\{t\-1\}&#41;^2\+&#40;y\_t\-y\_\{t\-1\}&#41;^2\}
&#93;

再根据 Bounding Box 尺寸进行归一化：

&#91;
v\_t=
\\frac\{D\_t\}
\{\\Delta t\\sqrt\{w^2\+h^2\}\}
&#93;

该速度不代表真实 m/s 或 km/h，只表示目标相对于自身视觉尺度的运动程度。

采用归一化速度可以降低目标远近和画面尺度差异的影响。

---

## 十、EMA 速度平滑

由于 YOLO Bounding Box 在静止目标上也可能轻微抖动，因此需要对视觉运动速度进行平滑。

采用 EMA：

&#91;
\\hat v\_t=
\\alpha v\_t\+
&#40;1\-\\alpha&#41;\\hat v\_\{t\-1\}
&#93;

EMA 参数作为算法内部固定参数，不暴露给普通用户。

### 平滑方法对比

|方法     |计算量  |内存      |抗抖动   |采用   |
|-------|----:|-------:|-----:|-----|
|不平滑    |最低   |O(1)    |较差    |否    |
|SMA    |低    |O(N)    |较好    |否    |
|**EMA**|**低**|**O(1)**|**较好**|**是**|
|中值滤波   |中等   |O(N)    |较强    |否    |
|Kalman |较高   |较高      |强     |否    |

前期考虑过“位置 EMA \+ 速度 EMA”的两级平滑。

最终考虑 ByteTrack 已能提供一定轨迹稳定能力，同时需要降低参数和实现复杂度，因此只保留：

> **视觉速度 EMA**

---

## 十一、运动状态与静止计时

根据 EMA 后的视觉速度，将目标划分为：

- MOVING
- STATIONARY

当速度低于内部静止阈值时进入 STATIONARY，并开始累计静止时间。

如果目标重新发生明显运动：

**STATIONARY → MOVING**

同时静止时间清零。

当连续静止时间达到用户设置的报警时间时：

**STATIONARY → ALARM**

报警依据为：

> **同一个目标连续静止的时间**

而不是目标在画面中的总出现时间。

---

## 十二、用户侧参数

用户只需要配置：

> **静止多久后报警**

例如：

|模式|报警时间|
|--|---:|
|灵敏|10 秒|
|标准|30 秒|
|宽松|60 秒|

其余参数均在系统内部固定：

|参数             |用户设置 |
|---------------|-----|
|YOLO Confidence|否    |
|ByteTrack 参数   |否    |
|EMA 系数         |否    |
|静止速度阈值         |否    |
|静止报警时间         |**是**|

---

## 十三、第二阶段验收

### 楼道消防通道

|场景     |期望结果      |
|-------|----------|
|人携带纸箱经过|MOVING    |
|推车经过   |MOVING    |
|纸箱被放下  |STATIONARY|
|长时间静止  |ALARM     |
|杂物重新移动 |MOVING    |

### 消防车通道

|场景    |期望结果      |
|------|----------|
|车辆正常通过|MOVING    |
|车辆缓慢通过|MOVING    |
|车辆停车  |STATIONARY|
|长时间停车 |ALARM     |
|车辆重新启动|MOVING    |

---

## 十四、第三阶段：算法迁移

第三阶段将 YOLO11n RKNN 模型、ByteTrack 和静止目标后处理整体迁移至 RK3588 C/C\+\+ 环境。

开发语言：

**C / C\+\+**

运行环境：

**RK3588 Linux**

目标：

> **形成一套可以在 RK3588 上独立运行的最小推理工程 Demo。**

该 Demo 不仅面向本地测试视频，还需要为后续实际工程中的 **RTSP 摄像机视频流**提供统一输入接口。

---

## 十五、MediaReader 媒体输入模块

媒体输入模块统一命名为：

> **MediaReader**

MediaReader 作为整个推理链路的统一媒体输入层，负责屏蔽不同视频源之间的差异。

计划支持：

|输入类型    |用途        |
|--------|----------|
|本地视频文件  |开发、调试、回归测试|
|RTSP 网络流|实际摄像机接入   |
|后续其他媒体源 |根据真实工程需要扩展|

MediaReader 对下游统一输出解码后的视频帧，使检测、跟踪和后处理模块无需关心数据来自文件还是 RTSP。

推荐职责包括：

- 媒体源打开与关闭
- 视频解码
- 视频帧获取
- 时间戳管理
- RTSP 断线处理接口预留
- 本地视频 EOF 处理
- 统一图像格式输出

实现上可结合 FFmpeg、GStreamer 或设备现有媒体框架，具体方式根据 RK3588 软件环境确定。

---

## 十六、C/C\+\+ 推理链路

整体处理链：

**MediaReader**

↓

**视频解码**

↓

**图像前处理**

↓

**YOLO11n RKNN / RK3588 NPU**

↓

**YOLO 后处理**

↓

**ByteTrack**

↓

**归一化视觉速度**

↓

**EMA**

↓

**静止计时与状态机**

↓

**结果绘制 / 事件输出**

---

## 十七、C/C\+\+ 模块划分

|模块              |功能                              |
|----------------|--------------------------------|
|**MediaReader** |本地视频 / RTSP 等媒体源读取与解码           |
|Preprocess      |Resize、Letterbox、颜色格式转换         |
|RKNN Detector   |YOLO11n RKNN 模型加载及 NPU 推理       |
|YOLO Postprocess|检测结果解析、置信度过滤、NMS                |
|ByteTrack       |多目标跟踪及 Track ID 管理              |
|Motion Analyzer |归一化视觉速度计算及 EMA                  |
|Track State     |MOVING / STATIONARY / ALARM 状态维护|
|Alarm Logic     |静止超过设置时间后生成事件                   |
|Visualizer      |Bounding Box、Track ID、状态等结果绘制   |
|Main / Pipeline |完整算法链组织                         |

其中 **MediaReader 应与检测算法解耦**，后续从本地视频切换至 RTSP 时，不修改 YOLO、ByteTrack 或静止目标检测逻辑。

---

## 十八、最小工程 Demo 要求

第一版最小 Demo 至少需要支持：

1. 从本地视频文件读取媒体；
2. MediaReader 设计上支持后续接入 RTSP；
3. 使用 RK3588 NPU 执行 YOLO11n RKNN 推理；
4. 输出目标 Bounding Box；
5. 为目标维护 Track ID；
6. 独立判断 MOVING / STATIONARY / ALARM；
7. 显示静止持续时间；
8. 超过用户配置时间后触发报警状态；
9. 支持楼道模型和消防车通道模型切换；
10. 能完整推理一段测试视频；
11. 输出基础性能统计。

完成最小 Demo 后，再验证 RTSP 实时流输入。

---

## 十九、第三阶段性能验证

在 C/C\+\+ 完整链路下统计：

|指标                |内容            |
|------------------|--------------|
|MediaReader 解码耗时  |媒体读取及解码       |
|Preprocess 耗时     |图像前处理         |
|YOLO 推理时间         |RK3588 NPU    |
|YOLO 后处理时间        |输出解析及 NMS     |
|ByteTrack 时间      |多目标跟踪         |
|Motion Analyzer 时间|速度 + EMA + 状态机|
|单帧总耗时             |完整处理链         |
|FPS               |最终实际帧率        |
|CPU 占用            |完整工程运行负载      |
|内存占用              |完整进程峰值        |

如后续接入 RTSP，还应额外观察：

- 网络流延迟
- 解码稳定性
- 断流恢复能力
- 长时间运行稳定性

---

## 二十、第三阶段交付物

最终交付：

- RK3588 C/C\+\+ 最小工程源码
- 楼道 YOLO11n RKNN 模型
- 消防车通道 YOLO11n RKNN 模型
- **MediaReader 模块**
- ByteTrack 模块
- Motion Analyzer 模块
- Track State / Alarm 模块
- CMake 构建文件
- 可执行 Demo
- 测试视频
- 编译运行说明
- 完整性能测试结果

MediaReader 至少保留：

> **本地视频 + RTSP**

两种媒体输入能力的接口设计。

---

## 二十一、最终技术路线

### 第一阶段：模型训练及端侧验证

**数据集 → YOLO11n 迁移学习 → PC 模型评估 → ONNX → RKNN → RK3588 精度 / 性能 / 内存验证**

### 第二阶段：静止目标判断

**YOLO → ByteTrack → 归一化视觉速度 → EMA → STATIONARY → 静止计时 → ALARM**

### 第三阶段：C/C\+\+ 算法迁移

**MediaReader → YOLO11n RKNN → ByteTrack → EMA → 状态机 → RK3588 最小 Demo**

最终形成：

> **两个 YOLO11n 垂直检测模型、一套轻量静止目标后处理算法，以及一套以 MediaReader 为统一媒体入口、能够扩展到 RTSP 实时流的 RK3588 C/C++ 最小推理工程。**