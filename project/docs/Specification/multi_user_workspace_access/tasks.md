# 实施任务：三类用户与工作空间授权

> 关联合同：[spec.md](spec.md)；关联设计：[design.md](design.md)<br>
> 本文记录实施顺序；T-02（基线入口）与 T-13（分层及图示文档）已完成。T-16 正在推进 Atlas application/adapter 切片，新增代码目前只有 ARM64 编译证据，REQ/AC 尚无新验收通过。

## 任务清单

### 1. 产品与共享合同

- [x] **T-01 锁定产品待确认项** — 已确认 PC 自助注册与管理员受控创建并存、移动账号仅经邀请、邀请 24 小时单次使用、设备级视频授权、健康服务下 30 秒撤权断流、管理端不开放部署修改/服务重启、移动成员设备数不设静态硬上限，以及停用 PC owner 后限制整个工作空间且本期不转移所有权。已回写 proposal；服务组合同同步见 T-03。覆盖 REQ-01/AC-01.4、REQ-03/AC-03.1、REQ-05/AC-05.3、REQ-07、REQ-09/AC-09.3；依赖：无。
- [x] **T-02 标注基线与扩展入口** — 已更新 `Analysis/PRD.md`、`Analysis/SPEC.md`、`Design/STRUCTURE.md` 和 `Specification/README.md`，标出当前单账号实现与三角色目标的状态差异并链接本组；产品待确认项锁定后的最终合同同步仍依赖 T-01/T-03。覆盖 REQ-01–REQ-09；依据：本组四份文档及上述入口文件。
- [x] **T-03 更新受影响服务规格组** — 已在 auth-service、device-service、media-service、event-service、push-service、web_pwa_client、platform_deployment 的 proposal/spec/design/tasks 中链接共享授权合同，区分 `workspace_id` 资源租户边界与 session/viewer/actor/Push `account_id` 用户主体语义，并为各组新增对应实施任务。八个相关规格组结构校验通过；实现与验收仍由 T-04–T-12/T-14–T-17 追踪。覆盖 REQ-01–REQ-09；依赖：T-01、T-02。

### 2. 身份、授权与数据迁移

- [~] **T-04 auth-service 身份、邀请和授权合同/实现** — 已加 `accounts.user_type/status` 兼容迁移与 active-only 会话校验；自助注册固定 `pc` 并事务创建 owner UUID workspace，启动时幂等回填旧 PC workspace；邀请 24 小时单次使用、移动账号绑定和设备 owner 在线校验已实现。新增 workspace 摘要、PC 成员/邀请查看、创建/撤销邀请、替换成员设备能力及撤销成员 API，授权接口逐次检查 session/account/owner/workspace/member/device grant。owner 停用时 workspace 进入受限态，移动成员不能读取摘要或兑换邀请。首次管理员由内部一次性 bootstrap 创建；管理 API/UI 支持创建 PC、停用/恢复 PC/mobile、健康探针汇总和审计查看；停用账号及撤销 workspace member 会在事务内撤销现有 session 并写 `session.revoked` outbox，由 auth worker 投递 media/push inbox。仍缺真实隔离数据库集成验收、理由/失败审计矩阵和目标部署探针验证。覆盖 REQ-01、REQ-03、REQ-04、REQ-07 / AC-01.1–01.3、AC-03.1–03.5、AC-04.4、AC-04.5、AC-07.1–07.3；依赖 T-01–T-03。
- [~] **T-05 服务 schema 扩展、回填和兼容读写** — 已为 device、camera、algorithm assignment、activation ticket、command、config snapshot、provisioning job、broker credential、device outbox、media stream、event/outbox/evidence/disposition 增加 nullable `workspace_id` 扩展列，并以已确认的 workspace-owner/父资源关系做幂等启动回填；新写入路径同步写租户字段。新增只读审计脚本 [`project/scripts/audit_workspace_migration.sql`](../../../scripts/audit_workspace_migration.sql)，按表报告空租户字段及孤儿/父子租户不一致数量。尚未运行真实数据库审计、双读一致性核对、非空/索引切换及副本回滚演练；未证明迁移可在真实旧库安全完成。覆盖 REQ-02、REQ-09 / AC-02.1–02.3、AC-09.1–09.5；依赖 T-01、T-03、T-04。
- [~] **T-06 device/media 成员授权和撤权** — media-service 已在建立/续租播放 lease 及 Nginx 媒体授权请求时通过 auth-service 校验 `stream.view`；撤销 session 的 outbox 会标记媒体 lease 待踢除，MediaMTX WebRTC/HLS 控制接口按 lease ID 查找并主动 kick；其他 capability 撤销后续租拒绝，25 秒 lease 与 1 秒扫描负责到期踢除。device-service 成员 `device.read` 列表/详情/统计使用 auth-service grants，移动视图省略设备 IP、算法分配和配置字段；新增只需 `stream.view` 的安全摄像头目录，不要求同时授予 `device.read`。媒体来源配置仍 owner-only、`workspace_id` 迁移未做；真实 MediaMTX/WHEP/HLS 断流与 30 秒上界尚未实测。覆盖 REQ-02、REQ-04、REQ-05 / AC-02.2–02.4、AC-04.1–04.5、AC-05.1–05.4；依赖 T-04、T-05。
- [~] **T-07 event/push 授权范围和接收人计算** — event-service 已通过 auth-service 按 session、设备和精确 capability 过滤移动事件列表/统计；详情、证据读取和处置分别检查 `event.read`/`event.handle`，处置保存实际 `actor_account_id`。event.created outbox 加入可信 `device_id`。push-service 收件箱接收时按事件设备向 auth-service 分页重算当前移动成员 `alert.receive` 收件人，只为其真实订阅建 delivery；每次发送前重新校验订阅账号、session 和设备权限，撤权拒发，授权依赖故障则重试。仍缺 `workspace_id` 迁移、成员撤销广播/媒体主动 kick 和运行时跨服务验收；本轮仅 Go build 编译证据，未运行测试。覆盖 REQ-03、REQ-04、REQ-06 / AC-03.3–03.6、AC-04.1–04.5、AC-06.1–06.5；依赖 T-04、T-05。

### 3. 管理端、PC 与移动 PWA

- [~] **T-08 管理员账号、健康与审计界面/API** — 已有一次性内部管理员 bootstrap；admin API/UI 支持账号清单、创建 PC、停用/恢复 PC/mobile、审计列表以及五服务健康探针和依赖状态；停用 PC 会限制 workspace，admin 授权不进入客户资源 API。健康未知/不可达以状态呈现，不提供部署修改或重启。仍缺真实部署探针核验、健康状态细化、理由/完整高风险审计以及浏览器权限/脱敏验收；本轮仅 Go/Web build 证据。覆盖 REQ-01、REQ-04、REQ-07 / AC-01.2–01.3、AC-04.3、AC-07.1–07.4；依赖 T-04、T-03。
- [~] **T-09 角色路由与完整业务 UI** — 已新增按服务端 `/auth/me` 角色分流 `/admin`、PC 工作台与 `/m`；PC 成员授权/邀请与撤权页面、邀请兑换页、工作空间名称编辑已接入；移动页面分别按 `device.read` 和 `stream.view` 显示安全设备信息与设备级摄像头目录，不要求视频用户额外拥有设备读取能力；事件读取/处置、单路视频和 Push 订阅入口已接入；管理员账号、健康、审计页已建。仍需完成 Push 深链/账号切换回归和真实浏览器/手机验收；已有原设备 SSH 指纹/引导/激活、RTSP 来源和监控工作流未重构回归。本轮执行 Web 单测及生产构建，名称编辑 API 增加集成用例（未因无隔离数据库执行）。覆盖 REQ-03、REQ-08 / AC-03.1–03.6、AC-08.1–08.8；依赖 T-04、T-06、T-07。

### 4. 验证、发布与回写

- [ ] **T-10 跨角色/跨租户安全回归** — 为 admin/PC/mobile × 每类 capability × 自有/他人资源建立服务端矩阵；覆盖伪造头、IDOR、越权截图/媒体、邀请边界、Push 收件人变更、账号停用和并发处置；依赖 T-04–T-09；覆盖 REQ-01 / AC-01.1、AC-01.2、AC-01.3、AC-01.4；REQ-02 / AC-02.1、AC-02.2、AC-02.3、AC-02.4；REQ-03 / AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-03.5、AC-03.6；REQ-04 / AC-04.1、AC-04.2、AC-04.3、AC-04.4、AC-04.5；REQ-05 / AC-05.1、AC-05.2、AC-05.3、AC-05.4；REQ-06 / AC-06.1、AC-06.2、AC-06.3、AC-06.4、AC-06.5；REQ-07 / AC-07.1、AC-07.2、AC-07.3、AC-07.4；REQ-08 / AC-08.1、AC-08.2、AC-08.3、AC-08.4、AC-08.5、AC-08.6、AC-08.7、AC-08.8；REQ-09 / AC-09.1、AC-09.2、AC-09.3、AC-09.4、AC-09.5。
- [ ] **T-11 迁移恢复与目标环境验收** — 隔离数据库备份/恢复和回滚演练；核对旧账号资源、孤儿清单和双租户统计；实机验证既有 Atlas 激活/心跳/规则 ACK 未受影响；验证真实 RTSP、WebRTC/HLS、移动端 Push 及撤权断流；记录 12 源/24 连接容量证据和未完成限制；覆盖 REQ-05、REQ-06、REQ-09 / AC-05.3–05.4、AC-06.3–06.4、AC-09.1–09.5；依赖 T-05–T-10。
- [ ] **T-12 文档与部署终态回写** — 依据实现更新所有受影响服务规格、PRD、全局 SPEC、STRUCTURE、部署指南和本组任务状态；仅对有证据的 AC 标记通过，保留未验收硬件/手机/容量边界；覆盖 REQ-01–REQ-10；依赖 T-10、T-11、T-14–T-17。

### 5. 架构分层与模块边界

- [x] **T-13 现状源码地图、目标分层与图示** — 核对 Go/Web/Atlas 当前目录，分别标明已存在边界与未来代码层；更新 PRD、全局 SPEC、STRUCTURE 和设计规格，并提供架构、用例、类、时序、状态 SVG。此项只完成文档评审，不代表 AC-10 代码验收通过；覆盖 REQ-08、REQ-10；依据：PRD §4.2.1、SPEC §12、STRUCTURE §§12–13、本组 proposal/design/spec；依赖：T-02。
- [ ] **T-14 Go 服务分层重构** — 以服务为边界抽出 application/domain/transport/adapters，先做可回滚垂直切片，保持 API、消息和各自 schema 写入合同；覆盖 REQ-10 / AC-10.1、AC-10.2、AC-10.3、AC-10.4、AC-10.8；依赖：T-03–T-07、T-13。
- [ ] **T-15 Web/PWA feature 边界与角色界面** — 让 pages/layouts 通过 feature use cases/state 和 typed API clients 调用后端；保留完整 PC 管理流程、成员设备授权及 mobile 单路视频 UI；覆盖 REQ-08 / AC-08.5–08.8、REQ-10 / AC-10.5、AC-10.8；依赖：T-09、T-13。
- [ ] **T-16 Atlas runtime 适配器边界** — 按硬件/协议 adapters、runtime application 和稳定算法插件接口渐进整理，回归激活、MQTT、规则 ACK、事件和 RTSP 链路；覆盖 REQ-10 / AC-10.6、AC-10.8；依赖：T-13。进度（2026-10-29）：新增告警事件发布用例与 ports、租期 gate、AgentMqttInbox/AgentMqttSession/AgentCloudCycle、AgentStatusReader、AgentCloudCommandProcessor/AgentCommandAcknowledger/AgentAppliedRuleAcknowledger、AgentServiceLifecycle/ActivationLeaseWriter、Unix socket client/server、SQLite outbox、libmosquitto transport、HTTPS evidence uploader/transport、SystemdAgentServiceLifecycle/FileActivationLeaseWriter、云规则配置/applied-state/租期文件/证据文件/事件元数据 adapters；main 与 VideoStreamManager 共用租期状态 port，Agent 本机控制通过 `LocalAgentControl`，持久事件队列通过 `DurableAgentEventOutbox`，MQTT lifecycle/TLS/publish/subscribe 经 transport port，连接状态/订阅/topic/回调由 application session 管理；命令/ACK 解析与 16 条有界命令队列由 inbox 管理，队列消费及拒绝 ACK 由 command processor 负责，ACK payload 由 acknowledger 组装，云规则配置适配器在共享互斥锁内匹配已启用模型、校验安装状态、落盘并重载配置。已应用规则 ACK 用例在同一配置锁内确认全部目标 runtime 状态、刷新 outbox 配置、按既有顺序发 ACK、写就绪标记并记录 command ID。新 `AgentCloudCycle` 通过 `AgentStatusReader` 汇集 connected cycle：处理命令、尝试规则 ACK、发布状态、发布到期事件并记录尝试、上传已确认事件证据，保持原顺序。激活 CLI/轮询现通过 `HttpsTransport` port 注入 `CurlHttpsTransport` adapter，保留 CA 验证、禁止跳转/代理、响应体上限和请求超时合同；固定 systemd `enable --now`/`restart` 操作通过 `AgentServiceLifecycle` 注入 `SystemdAgentServiceLifecycle`；租期 JSON 通过 `ActivationLeaseWriter` 注入 `FileActivationLeaseWriter` 写入，权限、原子替换与序列化位于 adapter。`AgentActivationWorkflow` 已从 activation 命令接管 MQTT grant 校验、broker URL/租期解析及配置/租期更新顺序；`activation.cpp` 保留 HTTPS、CLI 和 JSON 转换。WSL application source C++17 语法检查通过；Atlas 200I DK A2 `yunshao-agent` ARM64 target 已重新配置、编译并链接成功，包含 activation CLI/transport、新 workflow 和 `AgentRunLoop`。未运行激活/续期或服务操作。`agent.env` 与激活覆盖文件读写由 `AgentConfigurationStore`/`FileAgentConfigurationStore` 负责。截图 HTTPS 上传由 `EvidenceUploader` port 注入 adapter。AIModelPlugin 从 AIProcessor 抽出并加入 API 版本检查。Atlas 200I DK A2 `yunshao-agent` ARM64 target 成功编译/链接新配置存储、activation workflow 和 AgentRunLoop；本轮 `yunshao-classroom-runtime` ARM64 target 也重新编译/链接 `EvidenceFrameEncoder`/`AtlasEvidenceFrameEncoder`。未运行 CTest、安装或 runtime 回归。板端存在 clock-skew warning。未运行 CTest、激活/续期流程、运行时回归或重启服务；Agent 外层调度和激活 workflow 已提取并通过 Atlas ARM64 Agent build；证据帧编码 hardware adapter 已通过 Atlas runtime build；其余媒体/硬件边界仍需处理，并验证激活、规则 ACK、事件和 RTSP 全链路，任务未完成。
- [ ] **T-17 依赖图和自动架构门禁** — 已新增 `project/scripts/check_architecture.py` 与 GitHub Actions workflow：校验五个 Go module 之间无源码依赖、Go 包依赖图无环、Atlas application/ports 不导入 adapters/runtime manager。Web feature boundary 与 Go domain/adapters 分层 gate 等 T-14/T-15 落地后再启用；当前 gate 已在本机通过，不代表完整目标边界已成形；例外期限机制尚未实现。覆盖 REQ-10 / AC-10.1、AC-10.2、AC-10.3、AC-10.4、AC-10.5、AC-10.6、AC-10.7；依赖：T-13–T-16。

## 当前进度与后续顺序

当前已推进 **T-04、T-06、T-07 的授权垂直切片**：邀请设备经过 device-service 所有权验证；device/media/event/push 的实时或发送前设备 capability 检查已接入 auth-service。后续按以下依赖顺序继续：

1. **先闭合 T-04/T-08**：成员撤权现在也撤销其 sessions 并写 transactional outbox；下一步以隔离 PostgreSQL 验证事务回滚、auth→media/push 重试与完整审计矩阵，并完成部署健康验收。
2. **再做 T-05–T-07**：已把设备/摄像头/主要设备子资源、媒体来源和事件/outbox/证据/处置记录纳入 nullable `workspace_id` 回填，并加入只读迁移审计 SQL；下一步在隔离旧库运行计数、孤儿检查、双读核对及回滚演练，再完善授权矩阵、media 30 秒断流实测与 event/push 跨服务验收。
3. **再闭合操作界面 T-09**：摄像头目录现由独立 `stream.view` API 提供；补工作空间资料，回归既有设备 SSH/激活/RTSP/监控流程、Push 深链，并通过真实浏览器和手机验证日常操作都能经界面完成。
4. **最后做 T-10–T-12**：跨租户安全矩阵、迁移恢复与硬件/媒体容量验收，再按证据回写规格和部署文档。
5. **架构工作 T-14–T-17 持续单独追踪**：目前 T-16 有 Atlas ARM64 构建切片；未完成运行时回归、Go/Web 分层和自动门禁，不应据此标记架构验收通过。

## 验收记录

| AC 范围 | 检查方法 | 结果/证据 | 状态 |
|---|---|---|---|
| REQ-01–REQ-04 | auth/API 集成测试、权限矩阵、头伪造和服务故障注入 | 账号类型、邀请创建/兑换、设备 owner 查询、session-bound capability API 已实现初版；本轮没有运行 auth/device 集成测试或故障注入 | 代码部分实现；本轮未执行验收 |
| REQ-05 | MediaMTX/浏览器端到端播放、撤租与容量实测 | media-service 建租约、续租和媒体 auth 子请求已调用 `stream.view` 判定；25 秒租约与 1 秒到期扫描为代码配置，尚无真实 MediaMTX/浏览器断流或 30 秒上界证据 | 代码部分实现；端到端未执行 |
| REQ-06 | 事件/截图/处置/PWA Push 跨角色集成 | event-service 已接入 grant 过滤与真实处置 actor；push-service 已按 auth-service 当前设备授权解析移动接收人并发送前复核。仅 Go build 编译证据；未运行集成测试、证据撤权或真实浏览器 Push | 代码部分实现；未验收 |
| REQ-07–REQ-08 | admin 权限/脱敏、桌面和真实手机路由与通知点击 | 有 bootstrap、账号/状态/审计/health API 与角色化页面构建；未运行权限/脱敏/浏览器/手机验收，admin health 依赖本机服务探针仍待目标环境校验 | 代码部分实现；未验收 |
| REQ-09 | 数据库副本迁移、计数/孤儿检查、回滚及旧账号回归 | device 子资源、media stream、event/outbox/evidence/disposition 已加 nullable 扩展列与幂等回填；新增只读 SQL 审计脚本，可输出空 workspace 及父子租户不一致数量，但因无隔离 PostgreSQL 未执行；未运行旧库副本迁移、双读核对或回滚 | 迁移代码部分实现；未执行数据库验收 |
| REQ-10 | Go/Web/Atlas 依赖边界、静态门禁和垂直切片回归 | Atlas application/adapter 切片（含 MQTT inbox/session、AgentCloudCycle/AgentStatusReader、AgentServiceLifecycle/SystemdAgentServiceLifecycle、ActivationLeaseWriter/FileActivationLeaseWriter、cloud command processor/acknowledger、applied-rule ACK 用例与状态 adapter、云规则配置 adapter、HTTPS evidence uploader/transport）已在目标板完整 ARM64 CMake build 中编译并链接 agent/runtime；新增 `AgentConfigurationStore`/`FileAgentConfigurationStore` 已在本次 Atlas ARM64 `yunshao-agent` target 中编译并链接（未重建 `classroom-runtime`）；未运行 CTest 或运行时回归。Go 跨服务模块依赖/包循环、Atlas application 对 adapters/runtime manager 的 include 禁止已有自动门禁并在本机通过；Go domain/adapters 分层、Web feature 分层及 Atlas 硬件/媒体边界仍未成形，门禁不代表目标架构已达成；本轮新增 runtime rule-state reader/file adapter 尚未验证 | 部分实现；未验收 |

## 实施偏差与决定

| 日期 | 偏差/新发现 | 规格/设计更新 | 决定人或依据 |
|---|---|---|---|
| 2026-10-29 | 原方案的 PC 自助注册与管理员创建路径未区分；`account_id` 字段同时表示资源租户和用户主体。 | 已在 proposal 中记录并行创建规则及按字段语义分类的迁移原则；用户已确认 PC 自助注册与管理员创建并存。 | 仓库模型与路由；产品负责人确认（2026-10-29）。 |
| 2026-10-29 | Atlas 的 main/VideoStreamManager 曾重复处理租期文件和等待；告警事件/截图持久化、Agent Unix socket 协议处理、SQLite outbox 和 AIProcessor 动态库生命周期也缺少独立边界。 | 新增 alarm event publisher 与 lease gate use cases、ports，以及 socket client/server、SQLite/file/metadata/plugin adapters；VideoStreamManager 注入租期状态 port；Agent socket server 经 `LocalAgentControl` 访问状态和事件入队，Agent 依赖 `DurableAgentEventOutbox`，SQLite 实现移至 adapter；更新 Atlas README/spec/design 与本组现状图。插件接口仍是 C++ ABI，runtime 与插件须成对构建部署。 | 源码边界检查；本次未运行构建、测试或目标板验证。 |
| 2026-10-29 | Agent 直接管理 libmosquitto 连接、TLS、发布、订阅和 C callbacks。 | 新增 `MqttTransport` port 与 `MosquittoMqttTransport` adapter；Agent 改为通过回调接收连接/消息状态及通过 transport publish/subscribe，业务消息路由仍在 Agent。 | 源码边界检查；本次未运行构建、测试或目标板验证。 |
| 2026-10-29 | Agent MQTT callback 直接负责主题分流、ACK 应用和 16 条命令队列。 | 新增 application `AgentMqttInbox`，处理命令 JSON/有界队列与事件 ACK；Agent callback 现在转发消息，主循环从 inbox 取命令。 | 源码边界检查；本次未运行构建、测试或目标板验证。 |
| 2026-10-29 | `agent.cpp` 直接消费命令、选择模型并组装 command ACK；配置重载需要与状态读取互斥。 | 新增 `AgentCloudCommandProcessor`、`AgentCommandAcknowledger` 和 `CloudRuleCommandSettings` port/adapter。拒绝 ACK 字段、`command-acks` topic、QoS 1 与原共享锁语义保持；当时 applied-state 等待和成功 ACK 时机仍由 Agent 管理，随后由下条记录拆入独立用例。 | Atlas 200I DK A2 完整 ARM64 CMake configure/build 成功，两个目标均链接；未运行 CTest 或运行时 ACK 回归。 |
| 2026-10-29 | Agent 直接负责 applied-rule readiness 扫描、outbox 同步、成功 ACK 和就绪文件。 | 新增 `AgentAppliedRuleAcknowledger` application use case 与 `AppliedCloudRuleState` port/adapter；保留同一配置锁、runtime command ID 检查、outbox 更新在 ACK 前、就绪标记在 ACK 后的顺序。Agent 周期主循环仍负责调度。 | 修正 optional JSON 的显式构造后，Atlas 200I DK A2 完整 ARM64 CMake build 成功，两个目标均链接；未运行 CTest 或 MQTT 运行时回归。 |
| 2026-10-29 | `activation.cpp` 同时执行激活业务编排和 libcurl 请求。 | 新增 `HttpsTransport` port、move-only `SensitiveString` 和 `CurlHttpsTransport` adapter；一次性票据 POST 与带 Basic Auth 的激活轮询复用 HTTPS/TLS 限制，同时保留各自原请求超时。CLI/轮询流程和 systemd 操作仍在 `activation.cpp`。 | Atlas 200I DK A2 完整 ARM64 CMake configure/build 成功，两个目标均链接；未运行激活或轮询运行时回归。 |
| 2026-10-29 | Agent connected cycle 的命令/ACK、状态、事件及证据工作仍由 `agent.cpp` 顺序调用。 | 新增 `AgentStatusReader` port 与 `AgentCloudCycle` application service，将已连接后的云端操作按既有顺序集中编排；Agent 仍保留 session 启动、外层调度和激活轮询。 | Atlas 200I DK A2 完整 ARM64 CMake build 成功，Agent/runtime 均链接；未运行 CTest 或云端运行时回归。 |
| 2026-10-29 | 激活 CLI 直接调用 systemd 并拥有固定服务命令。 | 新增 `AgentServiceLifecycle` port 与 `SystemdAgentServiceLifecycle` adapter，由 agent main 组合根注入；保留 `enable --now` 后 `restart` 的既有命令行为。 | WSL C++17 语法编译通过；Atlas 200I DK A2 完整 ARM64 CMake configure/build 成功；未执行激活流程或 systemd 操作。 |
| 2026-10-29 | Agent 激活 CLI 和续期轮询直接负责授权租期序列化及文件权限/原子替换。 | 新增 `ActivationLeaseWriter` port 与 `FileActivationLeaseWriter` adapter；应用流程校验日期并传入 Unix 秒值，缺失/null/空字符串保留无限期语义；agent main 注入共享默认/环境覆盖路径。 | WSL C++17 语法编译通过；Atlas 完整 ARM64 CMake build 成功，未运行激活/续期流程。 |
| 2026-10-29 | `activation.cpp` 仍直接解析/更新 `agent.env` 并写入、删除激活覆盖文件。 | 新增 `AgentConfigurationStore` port 与 `FileAgentConfigurationStore` adapter；文件解析、保留未更新键、原子持久化与清理移出激活用例，调用方继续负责云端凭据校验和激活编排；密码环境项按值进入 adapter 并在其拥有范围内擦除。 | WSL Ubuntu 24.04 GCC C++17 语法检查与对象编译通过；Atlas CMake configure 与 `yunshao-agent` ARM64 build 成功；未运行激活流程或服务操作。 |
| 2026-10-29 | Agent 外层调度仍混在 `Agent::run`。 | 新增 `AgentRunLoop` application service，保留 15 秒激活轮询、5 秒循环周期与停止信号处理的组合根注入。 | Atlas 200I DK A2 `yunshao-agent` ARM64 build 成功；未运行 CTest 或运行时回归。 |

## 完成摘要

- 已完成任务：T-02 基线状态标注与规格组入口；T-03 受影响服务规格同步；T-13 现状源码地图、目标分层和 UML 文档更新。T-16 已推进 runtime use case/ports 与多类 adapter；Atlas ARM64 `yunshao-agent`/runtime 构建证据见上文，但任务和运行时验收未完成。
- 本轮构建/检查：device/event/auth/media/push 五个 Go 服务 `go test ./...` 通过；Web `npm test`（14 项）和 `npm run build`（含 Vue/Service Worker 类型检查）通过；架构静态门禁、规格组结构校验与 `git diff --check` 通过。PostgreSQL 集成测试因未配置隔离 `TEST_DATABASE_URL` 而跳过，迁移审计 SQL 未在真实数据库执行；CTest、设备运行时、激活或 MQTT ACK 验收亦未执行。目标板构建历史上存在时钟偏差警告。
- 未完成验收：新 REQ/AC、数据库旧库迁移/回滚、跨服务授权和撤权、真实媒体 30 秒断流、真实浏览器/手机 Push、完整代码分层与自动门禁仍待执行。
- 已同步的权威来源：PRD、全局 SPEC、STRUCTURE、Specification README、受影响服务规格均链接本组并区分当前实现与验收缺口；本轮部署文档新增管理 API 同源代理说明。
- 完成日期：本轮文档与功能切片已提交；整体重构验收尚未完成。
