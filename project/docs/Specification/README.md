# Specification 文档组

`Specification/` 按服务或领域维护长期有效的产品行为与工程合同。每组的四份文档各有唯一职责：`proposal.md` 说明目标和范围，`spec.md` 定义需求与验收，`design.md` 说明必要的技术选择，`tasks.md` 记录实施与验证状态。它们共同描述同一组工作，不能各自定义另一套需求、接口或状态。

代码与规格通过 Git 分支、提交和 PR 留下修改历史。开发时更新现有服务/领域组；只有新服务或新领域确实没有合适的组时，才初始化新组。不要为每次代码修改复制整套规格。

## 目录结构

```text
project/docs/Specification/
├── README.md
├── WORKFLOW.md
├── COMPARISON.md
├── auth_service/
│   ├── proposal.md
│   ├── spec.md
│   ├── design.md
│   └── tasks.md
├── device_service/
├── event_service/
├── media_service/
├── push_service/
├── platform_deployment/
├── atlas_edge_runtime/
├── classroom_behavior_detection/
├── web_pwa_client/
├── multi_user_workspace_access/  # 三类身份、成员授权与跨服务迁移（评审目标）
├── fire_access_obstruction_detection/  # 后续场景/历史规格，非首发
└── project_spec_workflow/
```

当前首发产品范围为教室行为检测，详见 [课堂行为检测](classroom_behavior_detection/)。Atlas 云边代理与模型运行时见 [Atlas 边缘运行时](atlas_edge_runtime/)，Web/PWA 交付见 [web_pwa_client](web_pwa_client/)。[消防通道规格](fire_access_obstruction_detection/) 保留为后续场景历史，不属于当前验收或实现顺序。

三类用户与工作空间授权是新评审目标，见 [multi_user_workspace_access](multi_user_workspace_access/)。该组集中记录当前实现证据、目标合同、建议架构和迁移任务；在产品待确认项解决并完成实现/验收前，其他服务规格记录的单账号行为仍代表现有基线。

首发五个 Go 服务以独立二进制和 systemd 运行，Web/PWA 以静态文件交由系统 Nginx 托管；PostgreSQL、Mosquitto 与 MediaMTX 原生运行。完整构建、配置和运维步骤见[平台部署规格](platform_deployment/)与仓库根目录 [DEPLOY.md](../../../DEPLOY.md)。

各组按需维护四份文档。轻量工作可以只改 `proposal.md`；没有新增技术选择时，`design.md` 写明沿用哪份合同即可。新服务/领域组可运行 Skill 的 `init_spec_group.py`，从仓库模板建立初始结构；脚本拒绝覆盖已有目录。

## 文档职责与事实来源

| 文档 | 负责回答 | 权威内容 |
|---|---|---|
| [工作流](WORKFLOW.md) | 本项目如何选择文档深度、评审、实现和验收 | 项目流程约定 |
| [调研比较](COMPARISON.md) | 为什么采用当前 Spec 范式 | 外部官方资料及项目取舍 |
| [产品需求](../Analysis/PRD.md) | 用户、产品范围和承诺 | 产品基线 |
| [研究记录](../Research/RESEARCH.md) | 外部证据和方案依据 | 研究基线 |
| [系统结构](../Design/STRUCTURE.md) | 服务职责、边界和数据流 | 架构基线 |
| [全局技术 SPEC](../Analysis/SPEC.md) | 跨服务接口、身份、数据和部署合同 | 平台技术基线 |
| 各领域组 | 服务/领域内行为、技术合同与实施状态 | 对应源码、接口和验证证据 |

遇到冲突时，产品范围以 PRD 为准，跨服务合同以全局 SPEC 为准，系统边界以 STRUCTURE 为准，服务内行为以对应领域组为准。目标规格不等于已交付事实；实现状态必须由代码、测试、日志或设备证据核对。

## 单组内的关系

- `proposal.md` 是范围与决策入口。对长期服务组，状态 `基线` 表示规格持续维护，不表示实现已完成；活跃工作可记录 `探索中`、`待评审`、`可实现`、`实现中`、`待验收`、`已完成` 或 `已取消`。
- `spec.md` 是唯一需求和验收定义处，需求用 `REQ-xx`，验收用 `AC-xx.n`。其他文档引用这些 ID，不另抄写或改变其含义。
- `design.md` 只写本次实现需要的技术决策；详细接口或数据合同若已在 `spec.md`，此处链接到对应章节。
- `tasks.md` 用 `T-xx` 引用 REQ/AC，记录实际任务进度和验收证据。未核查代码或设备时写“待核验”，不推断完成状态。

通常按 `proposal → spec → design → tasks → review → implement → verify → reconcile` 阅读和维护；轻量工作可跳过不必要的文档。每次只改动受影响的文件和唯一事实来源，不复制无关基线。

流程以 Markdown 和 Git 为基础，不要求安装 Spec Kit、OpenSpec、Kiro 或 BMAD。
