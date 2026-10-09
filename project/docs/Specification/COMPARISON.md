# Vibe Coding 中 Spec 工作流的形式与项目取舍

> 调研日期：2026-10-09。只比较公开、可复核的官方流程说明；不同工具/版本会演进。Vibe coding 没有单一强制标准，以下是常见的 Spec 驱动开发形态，而非完整市场普查。

## 项目背景

云哨是云边端视频事件平台，首发采用 RK3588，先围绕单个消防通道点位验证完整闭环，再扩展到最多 12 路活动视频通道。平台包含 Vue Web/PWA、Go/Gin/GORM 业务服务（auth、device、event、media、push）、Python/视觉算法与边缘设备链路，以及 Nginx、PostgreSQL、Mosquitto、MediaMTX 等基础设施。产品数据按个人账号隔离，实时媒体、设备消息、事件截图和 Web Push 各有独立边界。[PRD](../Analysis/PRD.md) [系统结构](../Design/STRUCTURE.md)

现有文档已包括产品 PRD、商用方案研究、系统结构、平台级技术 SPEC、服务级规格/实施任务书和算法专属说明。工作方式既有先明确接口合同再拆服务实现的文档驱动，也会在模拟联调、模型/边缘验证或小型原型中先获得证据，再修正规格。因此单一“先写完整规格再编码”的刚性流程不适配所有改动；同时，仅写一个提示词和任务清单会遗漏账号隔离、消息幂等、媒体授权、设备兼容等跨边界约束。

## 常见形式比较

| 形式 | 典型产物与循环 | 优点 | 代价/风险 | 对云哨的适配 |
|---|---|---|---|---|
| GitHub Spec Kit（完整 SDD） | 项目原则/constitution → specify → clarify → plan → tasks → analyze → implement；在规格、方案、任务间做检查 | 需求澄清和跨产物一致性有明确步骤；适合先确定产品/技术合同再实现 | 完整流程对小任务较重；命令和产物受工具约定影响，直接照搬会和已有 PRD、全局 SPEC 重复 | 采用澄清、需求追踪、实施前一致性检查；不强制其命令或 constitution 文件 |
| OpenSpec（变更增量） | explore → proposal → 按能力描述 spec 增量 → 可选 design → tasks → apply → archive；归档时把已交付增量合并回当前规格 | 围绕一次变更组织上下文；增量和长期基线分开，易审阅、续做、追溯；文本文件对 Vibe Coding 友好 | 增加变更包维护；若忘记回写/归档，基线仍会漂移；对产品发现和既有架构的详细治理较轻 | 作为日常变更包和归档主形态；用项目规则补足产品追踪及系统风险评审 |
| Kiro 功能规格/缺陷规格 | 功能：requirements → design → tasks；缺陷：分析现状/期望并形成修复设计与任务；任务逐项执行 | 功能与缺陷走不同路径；需求可写用户故事/验收条件；任务有依赖和进度 | 主要关注单个 feature/bug 包；长期规格基线、归档回写需项目自行规定；一次快速生成所有产物可能跳过有价值的评审 | 采用功能/缺陷双路径及当前/期望/回归关注点，不照搬 IDE 专属状态管理 |
| BMad（角色/层级文档链） | brief/PRD → UX/architecture spine → epic spec → tickets/build → review/retro | 明确产品真相源、架构基线和团队责任；适合多角色、较大项目、审计需要 | 角色和文档层级较多；小团队或一个人带 AI 开发时容易流程过重；“所有下游从上游派生”需持续维护 | 借鉴单一事实来源与变更落点规则；不引入完整角色编排 |
| 纯提示词/一次性 plan | 一段需求或 plan + 直接编码 | 启动快、适合小且可逆的改动 | 需求边界、失败情形、任务状态和规格回写依赖记忆；上下文切换后难续做 | 只用于低风险、局部、容易回滚的改动，并保留简短验收记录 |

以上过程描述来自各自官方文档：[Spec Kit workflow](https://github.github.com/spec-kit/reference/workflows.html)、[Spec Kit agentic SDD](https://github.github.com/spec-kit/reference/agentic-sdd.html)、[OpenSpec quickstart](https://openspec.dev/docs/quickstart)、[OpenSpec spec-driven schema](https://openspec.dev/docs/schemas/spec-driven)、[Kiro feature and bugfix specs](https://kiro.dev/docs/specs/)、[BMad organization planning](https://docs.bmad-method.org/plan/plan-inside-an-organization/)。访问日期均为 2026-10-09。

## 选择：风险分级的增量 Spec

采用 **OpenSpec 式变更包作为执行骨架，Spec Kit 式澄清/一致性分析作为标准路径检查点，Kiro 式功能与缺陷分流，BMad 式单一事实来源和责任落点**。变更包由普通 Markdown 构成，不依赖第三方 CLI。它和现有文档映射如下：

| 问题 | 事实来源 / 变更落点 |
|---|---|
| 用户是谁、解决什么问题、交付范围是什么 | 先看 `Analysis/PRD.md`；如需改变业务范围，先更新 PRD 并由业务责任人确认 |
| 方案依据是否有证据 | `Research/RESEARCH.md` 与相关研究记录；外部事实注明来源和访问日期 |
| 系统边界和高层组件如何协作 | `Design/STRUCTURE.md` |
| 跨服务长期接口、身份/数据/部署合同 | `Analysis/SPEC.md` |
| 某服务内部的详细合同和阶段实施计划 | 对应 `<service>_service_spec.md` |
| 本次要增加/修改/删除什么及其任务状态 | `Specification/changes/<date>-<slug>/`；完成后回写上述基线并移入 `changes/archive/` |

**优点**：每次 Vibe Coding 会话有明确上下文包；服务并行实现仍共享全局合同；规格承接可验收行为，任务清单承接实现状态；原型结论可以回补，避免把尚未验证的设计误写成已知事实；小改动不被完整文档链拖慢。

**代价与控制**：多一层变更包维护，并要求实现完成时显式更新长期文档。用按风险选择路径、只写变更差异、不复制上游文档、变更包与代码一起提交、任务验收后回写及归档来控制成本。未确认的产品决策标记 `[待确认]`，技术推断标记 `[假设]`，不得由 AI 静默定案。

## 文档边界说明

本组是为该项目拟定的工作约定，不宣称是某门课程或外部方法论的原文标准。现有 `Specification/` 为单数且已经被链接使用，因此保留路径；没有迁移到工具私有目录，也没有把服务实施任务书改写成新的重复基线。
