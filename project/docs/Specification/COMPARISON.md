# Spec 工作流调研与项目取舍

> 调研日期：2026-10-10。下列内容依据项目官方文档；工具流程会随版本演进。本节记录云哨的裁剪理由，不表示安装或采用这些工具。

## 常见模式

| 项目 | 常见流程与强项 | 对云哨的借鉴 |
|---|---|---|
| GitHub Spec Kit | Constitution、需求澄清、规格、计划、任务，以及跨文档一致性分析；适合把目标、约束和实施方案分开检查 | 先澄清范围；编码前检查需求、设计、任务之间的一致性；任务必须能追溯到可验收需求 |
| OpenSpec | 以需求提案、能力规格、设计、任务组成可审阅的规格驱动循环，强调规格差异与已交付系统合同的同步 | 每次工作只更新受影响的需求；完成时同步持久规格，避免规格和实现漂移 |
| Kiro | 功能规格分为需求、设计、任务；缺陷路径突出环境、复现、当前/预期行为和根因分析 | 功能和缺陷使用不同的提案关注点；根因未经验证时标为假设 |
| BMad | 从产品简报/PRD 到 UX、架构、规格和任务，强调上游责任与单一事实来源 | 让产品范围、研究证据、系统结构、跨服务合同和服务合同各自有唯一落点 |

公开资料：[Spec Kit 的 Agentic SDD](https://github.github.com/spec-kit/reference/agentic-sdd.html)、[OpenSpec 的 spec-driven schema](https://openspec.dev/docs/schemas/spec-driven)、[OpenSpec Quickstart](https://openspec.dev/docs/quickstart)、[Kiro Specs](https://kiro.dev/docs/specs/)、[BMad 组织内规划](https://docs.bmad-method.org/plan/plan-inside-an-organization/)。

## 云哨采用的范式

云哨使用按服务或领域分组的持久文档：`proposal.md` 描述领域目标与范围，`spec.md` 定义行为和验收，`design.md` 记录影响实现的技术选择，`tasks.md` 追踪当前实施工作。每次需求只改动相关组和受影响的全局基线；代码与文档通过同一 Git 分支、提交和 PR 留下历史。小改动允许精简文档，风险较高时增加追踪矩阵和兼容、安全、回滚及运维细节。

此设计融合了 Spec Kit 的澄清和一致性分析、OpenSpec 的增量同步理念、Kiro 的功能/缺陷分流、BMad 的事实来源责任划分，同时保留云哨已有的 PRD、Research、Design 和服务合同路径。它不要求每次开发都复制整套文档，也不另造 Git 已能提供的历史记录。

## 事实来源

| 信息 | 唯一维护位置 |
|---|---|
| 产品用户、范围和承诺 | `project/docs/Analysis/PRD.md` |
| 外部证据和研究结论 | `project/docs/Research/RESEARCH.md` |
| 高层系统边界和数据流 | `project/docs/Design/STRUCTURE.md` |
| 跨服务接口与技术合同 | `project/docs/Analysis/SPEC.md` |
| 服务/领域行为和技术约定 | `project/docs/Specification/<domain>/` |
| 每次修改的历史与审查 | Git 分支、提交、PR |

领域组中的目标说明不能替代对源码和运行证据的检查。实现与规格不一致时，先确认是实现缺陷还是需求决策改变，再更新负责该事实的唯一文档。
