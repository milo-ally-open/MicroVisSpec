# Atlas 200I DK A2 边缘运行时

本目录是 Atlas 200I DK A2（Ascend 310B4）的可部署工程。产品运行时由两个 systemd 进程组成：`yunshao-agent` 管理设备身份、MQTT、模型目录和 SQLite 事件队列；`yunshao-classroom-runtime` 管理视频解码、模型插件、逐帧推理、OSD 和告警规则。两者共享本机配置及模型目录，通过 `/run/yunshao/agent.sock` 传递命中事件。模型后端通过 `IAIModel` 插件接口装载，Atlas 实现使用 AscendCL/DVPP，不依赖 AX650 SDK。

## 从 Demo 到产品运行时

课堂检测 Demo 的职责是证明一条固定链路可运行：读取 H.264 视频、加载 YOLO11n/640 OM、执行 NPU 推理、按 `classes.txt` 映射类别并输出带框 H.264。它把视频源、单一模型、阈值、类别和结果输出都放在一个入口中，适合单场景验证，不适合作为长期运行的设备服务。

产品集成保留了模型推理与 Atlas 媒体能力，把 Demo 的入口拆成可复用模块：

| 产品位置 | 职责 | Demo 到产品的变化 |
|---|---|---|
| `include/ai_interface.hpp` | 通用 `IAIModel`、输入帧和检测结果插件 ABI | 把模型装载/推理从视频循环中解耦 |
| `plugins/model_classroom_behavior_detection.cpp` | `ModelClassroomBehaviorDetection`：YOLO11 AscendCL OM 装载、NV21 输入、张量解码、NMS | 类别顺序、输入尺寸、阈值和最大检测数从 profile 读取；插件名与 model_id 一致 |
| `include/manager/`、`src/manager/` | AI processor、推理/流/config/alarm managers | 按 stream 选 model_id，分开推理、媒体和业务规则 |
| `include/media/*.hpp`、`src/media/*.cpp` | 同名媒体接口与实现；Atlas VDEC/VPSS/VENC、FFmpeg 输入和 OSD | 按相同基名配对，媒体实现不放在 `include/` |
| `include/common/core.hpp`、`src/common/core.cpp` | 模型清单、SHA-256、Atlas SoC 校验、按 model_id 安装/激活/回滚、事件 outbox | 头源同名配对；不包含课堂类别、课堂事件类型或具体 SoC 常量；与 profile 对照验证 |
| `include/common/config.hpp`、`src/common/config.cpp` | 三份 JSON 的结构、能力集合和跨引用校验 | 头源同名配对；运行参数从配置读取；非法快照启动失败，不带入半份配置 |
| `src/agent/agent.cpp`、`src/main.cpp` | 云边 agent 与 classroom-runtime 组合入口 | 仅告警规则命中后，经本机 socket 将事件交给 agent 持久化 |

## 集成一个新模型

### 1. 准备模型产物

按 `project/preparation/model_classroom_behavior/WORKFLOW.md` 训练/取得权重并导出 ONNX；Atlas 推理使用面向 Ascend 310B4 的 OM。课堂模型转换入口为 `project/preparation/model_classroom_behavior/model_convert/convert_atc.sh`，输入 640×640 ONNX 并应用 `aipp_nv21.cfg`。转换后的 ONNX、OM、评估报告和数据集留在 `preparation/`，不放进产品源码或 Docker 镜像。

课堂示例使用已有文件：

- PT：`project/preparation/model_classroom_behavior/model_train/train_result/yolo11n_classroom_behavior_best.pt`
- ONNX：`project/preparation/model_classroom_behavior/model_export/artifacts/yolo11n_classroom_behavior_best.onnx`
- OM：`project/preparation/model_classroom_behavior/model_deployment/yolo11n_classroom_behavior_best.om`
- 当前 OM 清单：`project/preparation/model_classroom_behavior/model_deployment/model_classroom_behavior_detection.manifest.json`

课堂模型 ID 为 `model_classroom_behavior_detection`，类别顺序必须与训练/导出数据一致：`hand-raising`、`reading`、`writing`。新模型先确认 OM 输入布局、输出 decoder 与类别顺序；不匹配的模型不能复用课堂插件。

### 2. 登记模型 profile

在 `config/models.json` 的 `models` 数组增加 profile，至少填写唯一 `model_id`、云端算法键、模型格式/文件名/SoC、plugin library、backend、family、decoder、输入宽高/像素格式/预处理、有序 `classes`、置信度阈值、NMS 阈值和最大检测数。模型及插件统一按 `model_<domain>_<task>` 命名，task 使用 `detection`、`segmentation` 或 `classification`；例如本模型为 `model_classroom_behavior_detection`，库名为 `libmodel_classroom_behavior_detection.so`。profile 引用的插件库必须与 model_id 匹配。相关值必须属于同文件 `runtime.capabilities` 声明的支持集合。当前 Atlas 产品构建声明支持 `om`、`ascendcl`、`yolo11/yolo_v11`、NV21 `static_aipp` 和 310B4；输入尺寸为 640×640。

`algorithm_key` 表示云端算法语义（本模型为 `classroom_behavior_detection`），`model_family` 表示网络/输出族（本模型为 `yolo11`）；两者不代替 model_id。新增领域或任务时，使用相应命名的插件源文件、共享库和 `Model<Domain><Task>` 类；内部通用 YOLO 解码逻辑可以抽取复用，但不能用架构名作为产品插件名。新增插件须实现 `IAIModel`、导出 `CreateAIModel`/`DestroyAIModel`，并在 CMake 构建和 `runtime.capabilities` 中登记。禁止只改配置来宣称代码不支持的能力。

### 3. 绑定视频流和告警规则

在 `config/streams_config.json` 新增 stream，填 `stream_id`、`camera_id`、环境变量形式的 `source_ref`、profile 的 `model_id`、`live_source`、输出尺寸/帧率/目标码率、AI 帧率、输入读取超时、重连初始/最大退避和 RTSP 输入传输方式。实际视频输入 URL 通过 systemd 环境文件提供；RTSP 输出不配置用户名或密码。live 输入超时或断开后会记录不含 URL 的错误并按 profile 退避重连；本地文件 profile 可将 `live_source` 设为 false，EOF 后正常退出。示例流默认关闭。

课堂默认预览参数为 1280×720、15 fps、1500 kbps；runtime 从每路 stream profile 读取目标码率，`MediaPipeline` 不再固化编码码率。`input_timeout_ms` 控制 FFmpeg 网络读取上限；`reconnect_initial_delay_ms` / `reconnect_max_delay_ms` 控制断流重试退避；RTSP 可选择 TCP 或 UDP transport。

需要提供云端预览时，将该 stream 的 `rtsp_output.enabled` 设为 `true`，配置 `listen_address`、`port`、`mount_path` 和 `max_queue_ms`。服务以无需 RTSP 账号密码的 TCP interleaved RTSP 输出处理后的 H.264；客户端未连接时不排队，慢客户端超过缓冲上限时丢弃预览帧，不阻塞推理。RTSP 模式不持续写本地 H.264 文件；显式传入 `--output <path>` 才保存诊断视频。生产环境应限制 RTSP 监听端口只允许 MediaMTX 所在的受控园区网络访问。

Atlas RTSP 输出地址需由 media-service 所在网络实际可达。以板卡地址 `192.168.0.2` 和默认 stream 为例，RTSP 来源 URL 是 `rtsp://192.168.0.2:8554/classroom-demo`；在设备详情只提交该地址，MediaMTX 按 `sourceOnDemand` 匿名拉流。此地址示例只有在云端容器能路由到板卡所在园区网络时有效；不要将 Atlas RTSP 端口开放到公网。浏览器仍只取得受保护的 WHEP/HLS 播放地址。

在 `config/alarm_rules.json` 为需要告警的 model/class 添加 `event_type`、类别属性名、等级、置信度阈值、最短持续时间和冷却时间。新增规则必须先设为 `enabled: false`；未配置或禁用时仍可推理/OSD，但不写事件 outbox。只有明确启用且满足规则时才生成业务事件。

### 4. 生成清单、校验和入库

清单把模型文件和 profile 绑定起来：

```json
{
  "model_id": "model_classroom_behavior_detection",
  "version": "1.0.0",
  "format": "om",
  "model_file": "model.om",
  "sha256": "与 OM 文件一致的 SHA-256",
  "soc_version": "Ascend310B4",
  "input_width": 640,
  "input_height": 640,
  "classes": ["hand-raising", "reading", "writing"]
}
```

将清单和对应 OM 复制到 Atlas 后，以 `yunshao` 服务账号执行：

```bash
sudo -u yunshao /usr/local/bin/yunshao-agent model install --manifest /path/to/model_classroom_behavior_detection.manifest.json --model /path/to/yolo11n_classroom_behavior_best.om
sudo -u yunshao /usr/local/bin/yunshao-agent model status model_classroom_behavior_detection
```

`yunshao-agent model` 会自动读取 `/etc/yunshao/cann.env`；CMake 配置也从该文件取得 toolkit 与运行库路径。安装后的 ELF RPATH 解决进程启动前的共享库查找，因此无需在交互终端 `source set_env.sh` 或手动传递 `LD_LIBRARY_PATH`。systemd 服务仍通过各自的 `EnvironmentFile` 加载完整 CANN 环境。

安装器核对 JSON、SHA-256、profile 类别顺序、输入尺寸、OM 格式和 SoC；再用 AscendCL 在目标设备试加载。通过后写入 `/var/lib/yunshao/models/models/{model_id}/{version}/`，最后原子切换 `active/{model_id}`。失败时保留原活动版本。回滚使用 `yunshao-agent model rollback <model_id>`。

### 5. 构建和验证

在已安装 Atlas CANN、OpenCV、FFmpeg、SQLite、OpenSSL、Mosquitto、GStreamer RTSP Server、GStreamer App 开发依赖及 `rtph264pay` 插件的 Atlas/Linux 构建环境执行：

```bash
cd atlas200idka2
cmake -S . -B build
cmake --build build -j2
ctest --test-dir build --output-on-failure
```

先用 `yunshao-classroom-runtime --check-config` 验证三份配置，再以授权 H.264 样例源运行指定 `--stream-id`。开启 `rtsp_output` 后，用 `ffprobe` 匿名读取 `rtsp://<atlas-ip>:<port>/<mount>` 并确认画面可解码，再由 media-service 使用单一 RTSP 地址注册为 MediaMTX 按需来源；未接入真实相机和云端播放前不得宣称端到端验收完成。模型文件大小/哈希和 Atlas 是否能加载可以由安装结果确认；完整 ONNX/OM 测试集精度对比按需运行 `model_val/`，不作为模型注册的前置步骤。

开发依赖包（Debian/Ubuntu）包括 `libgstreamer1.0-dev`、`libgstreamer-plugins-base1.0-dev`、`libgstrtspserver-1.0-dev` 和 `gstreamer1.0-plugins-good`。运行镜像/板卡至少需提供 GStreamer core、app、RTSP server 库以及 `appsrc`、`rtph264pay` 插件。

## 端到端推流播放测试 SOP（Atlas → Windows）

按下面三步即可验证“Windows 测试视频 → Atlas 推理 → RTSP → Windows 播放”。告警规则保持关闭。本测试确认视频链路和模型运行时能工作，不用于评估模型精度。

### 开始前

- Atlas 已安装 CANN、运行依赖，且课堂模型已入库；检查命令应显示模型已安装并有活动版本：

  ```bash
  sudo -u yunshao /usr/local/bin/yunshao-agent model status model_classroom_behavior_detection
  ```

- Windows 已安装 FFmpeg，`ffmpeg`、`ffplay` 可在终端直接运行；测试视频在仓库根目录 `video/classroom_behavior.mp4`。
- Atlas `192.168.0.2` 与 Windows 网络互通，UDP/5000 和 RTSP/8554 可达。

### 1. 更新并安装板端程序（代码有变更时执行）

在 Windows 仓库根目录运行：

```text
scp -r project/src/device_sides/atlas200idka2 root@192.168.0.2:/root/
```

SSH 登录 Atlas 后运行：

```bash
cd /root/atlas200idka2
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j2
ctest --test-dir build --output-on-failure
sudo cmake --install build --prefix /usr/local
sudo systemctl daemon-reload
sudo systemctl enable yunshao-agent.service
```

安装会更新 `/etc/yunshao` 中的默认 JSON；板上有自定义配置时，先备份再安装。接下来只需改两份配置，无需额外建测试目录或 systemd 覆盖文件。

### 2. 在板端配置并启动测试流

编辑 `/etc/yunshao/streams_config.json`，只对 `classroom-demo` 将 `enabled`、`enable_ai` 和 `rtsp_output.enabled` 设为 `true`；保留已有的 `source_ref`、8554 端口和 `/classroom-demo` 路径。确认 `/etc/yunshao/alarm_rules.json` 中规则仍全部为 `false`。安装的默认配置会关闭这三个开关；只配置环境变量并重启服务不会启用视频流。

编辑 `/etc/yunshao/classroom-runtime.env`，只加入本次测试的输入来源地址：

```text
YUNSHAO_CAMERA_CLASSROOM_DEMO_URL=udp://0.0.0.0:5000
```

保存后，在板端启动服务并检查日志：

```bash
sudo chown root:yunshao /etc/yunshao/classroom-runtime.env
sudo chmod 0640 /etc/yunshao/classroom-runtime.env
sudo systemctl restart yunshao-classroom-runtime.service
sudo systemctl is-active yunshao-classroom-runtime.service
sudo ss -lntup | grep -E '(:5000|:8554)'
sudo journalctl -u yunshao-classroom-runtime.service -n 20 --no-pager
```

服务应显示 `active`，并监听 UDP/5000 与 TCP/8554。`rtsp_output_ready` 日志可作辅助信息；当前板端 `journalctl` 可能没有服务日志记录，因此日志为空不能单独说明服务未启动。服务此时只是在等待输入，还没有可播放的视频。

### 3. 从 Windows 推流并播放

必须让 RTSP 播放器先连上，再开始 UDP 推流。RTSP 输出是实时流；推流先结束、播放器后启动时，播放器不会回放之前发过的 UDP 数据。本板端的编码流需要客户端从首个关键帧开始接收参数集；播放器晚加入时可能报 H.264 PPS 错误并一直没有画面。

先在 Windows 终端 A 启动播放器，无需用户名或密码，保持终端运行：

```text
ffplay -rtsp_transport tcp "rtsp://192.168.0.2:8554/classroom-demo"
```

再在 Windows 终端 B 从仓库根目录开始推流，并保持推流运行：

```text
ffmpeg -re -stream_loop -1 -i video/classroom_behavior.mp4 -map 0:v:0 -vf "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2:black,fps=15" -an -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p -g 30 -bf 0 -b:v 700k -f mpegts "udp://192.168.0.2:5000?pkt_size=1316"
```

`ffplay` 应显示处理后的视频；检测框按模型实际结果显示。不要在播放器启动前结束推流。确认画面正常后，按 `Ctrl+C` 停止终端 B 的推流，再关闭终端 A 的播放器。

测试结束后，在板端关闭该流的三个开关、从环境文件移除本次测试的输入 URL，再运行 `sudo systemctl stop yunshao-classroom-runtime.service`。如果安装前备份过配置，恢复备份即可。不要删除已入库的模型。

## 部署与配置边界

### 局域网云端联调

使用云端创建的 `camera_id` UUID 填写 stream 的 `camera_id`，将登记时的稳定键填写为 `camera_key`。agent 用稳定键更新摄像头状态，事件用 UUID 关联云端资产。两者不能互相替代。

网页下发告警配置时，agent 校验设备、算法、模型版本、规则字段及配置版本，并原子更新 `alarm_rules.json`。runtime 每秒检查配置，在实际处理视频帧后写入带时间的运行状态；agent 确认当前配置已被活动流加载，再发送应用回执。活动状态超过 10 秒未刷新时，不继续宣称算法正在运行。板端配置目录必须允许 `yunshao` 用户写入，例如复制三份 JSON 到 `/var/lib/yunshao/config` 并将两个服务环境中的 `YUNSHAO_AGENT_CONFIG_DIR` 都设为该目录。

告警事件在 SQLite outbox 中保留本机路由信息，上报 MQTT 时转换为后端接受的事件字段。截图从触发告警的处理帧生成 JPEG，先保存在 `/var/lib/yunshao/evidence-outbox`；收到事件应用 ACK 后，通过 HTTPS PUT 上传截图，成功后删除本地待上传副本。JPEG 单张不超过 4 MiB，待上传目录限制 100 MiB。上传端点由 agent 环境变量 `YUNSHAO_EDGE_API_ORIGIN` 指定，例如 `https://172.20.10.4:19080`；沿用独立设备凭据校验 HTTPS；证书优先使用 `YUNSHAO_EDGE_API_CA`，未设置时回退到 `YUNSHAO_MQTT_CA`，不关闭证书校验。未设置 API Origin 时不尝试上传。

构建截图上传需要 `libcurl4-openssl-dev` 和 OpenCV `imgcodecs`。边缘 CTest 在 Release 构建中保持断言启用，覆盖云端命令校验、重试幂等、失败时保留原配置，以及内部事件到上报合同的转换。

RTSP 输出会在无人观看时保留 H.264 SPS/PPS，并在后续 IDR 前附带参数集。网页或其他播放器可以在推理已经运行后加入，无需重新推送整个测试视频。

- systemd 单元在 `systemd/`；CANN 运行库与算子路径由必需的 `/etc/yunshao/cann.env` 提供，主机身份、MQTT TLS 参数、HTTPS API Origin 和视频源 URL 分别由 `/etc/yunshao/agent.env`、`/etc/yunshao/classroom-runtime.env` 注入；用户激活/续期由网页发起，agent 以 `YUNSHAO_EDGE_API_ORIGIN` 和当前设备凭据轮询领取新租期及 MQTT 凭据，无需用户登录板端。轮换凭据保存在 `/var/lib/yunshao/activation.env`，由 systemd 后置 EnvironmentFile 加载；该文件不进入浏览器或 Git。首次接入时，园区用户可在设备详情页输入管理员提供的 SSH 用户名和密码；device-service 通过登记 IP 执行固定的 agent-only 部署及网页激活，任务结束后不会保存 SSH 密码。设备需预先安装 Atlas 驱动/CANN、开启 SSH 和 systemd，并能连通云端 HTTPS/MQTT。`cann.env` 必须从目标板已安装的 CANN 环境生成，权限设为 `root:yunshao 0640`；CMake 从此文件读取 toolkit 与运行库路径，安装二进制写入对应 RPATH；`yunshao-agent model` 在执行 ACL 操作前也会读取该文件。不要依赖登录 shell、Conda 或手工导出环境变量。CMake 安装会更新 `/etc/yunshao` 中的工程 JSON 配置，但不覆盖环境文件；保留板上对 JSON 的本地定制时，应在升级前备份并在安装后合并。Atlas CANN/ACL 当前不能与 runtime unit 的 `ProtectHome`、`PrivateTmp`、`ProtectSystem`、`ReadWritePaths` 文件系统隔离选项共同工作；这些选项须先在目标板通过 CANN 初始化及固定视频处理验收。unit 仍以非 root `yunshao` 运行，保留 `NoNewPrivileges=true` 并加入 `video`、`HwHiAiUser`、`HwBaseUser` 组。安装包提供 `udev/99-yunshao-rgn.rules`，将 `/dev/rgn` 设为 `root:HwHiAiUser 0660`；安装后重载 udev 规则并触发现有 `rgn` 节点，然后重启服务。`agent.env` 中的 `YUNSHAO_MQTT_CLIENT_ID` 必须使用 device-service 激活响应的原值，不能从 device_id 本地派生；MQTT 用户名、密码和 CA 同样来自激活/部署流程。Atlas 的 RTSP 输出不使用这些 MQTT 凭据，也无需额外配置用户名或密码。
- 模型 profile、流绑定、告警规则分别维护在 `config/models.json`、`config/streams_config.json`、`config/alarm_rules.json`。secret 不写入这些 JSON。
- `max_loaded_models` 默认 1。只有目标板多模型驻留、内存、延迟和稳定性验证通过后，才提高上限。
- 关键验证见 [Atlas 边缘运行时 Spec](../../docs/Specification/atlas_edge_runtime/spec.md) 与 [课堂行为检测 Spec](../../docs/Specification/classroom_behavior_detection/spec.md)。
