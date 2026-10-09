# Specification 文档组

本文档组规定云哨项目如何将一项需求或缺陷转成可审阅、可执行、可验收的开发工作。工作流借鉴了 Spec Kit 的澄清、方案分析和一致性检查，OpenSpec 的变更增量与归档，Kiro 的功能/缺陷双路径，以及 BMad 对上游依据和下游文档责任的划分；这些是基于公开资料的项目裁剪方案，不代表采用了这些工具或其原样流程。比较与取舍见[调研比较](COMPARISON.md)。

## 入口与文档职责

| 文档 | 用途 | 事实来源 |
|---|---|---|
| [工作流](WORKFLOW.md) | 选择轻量、标准、跨边界或缺陷流程；定义评审、实现、验收和归档 | 本项目执行约定 |
| [方案比较](COMPARISON.md) | 记录调研范围、常见形式、优缺点和选择理由 | 官方公开资料，访问日期 2026-10-09 |
| 模板 | 创建变更说明、行为规格、技术设计和任务清单 | 每项具体变更的工作文件 |
| [全局技术 SPEC](../Analysis/SPEC.md) | 跨服务技术合同与产品需求落点 | 平台级长期基线 |
| [系统结构](../Design/STRUCTURE.md) | 服务职责、系统边界、数据流和架构原则 | 平台级结构基线 |
| 服务规格 | [auth-service](auth_service_spec.md)、[device-service](device_service_spec.md)、[event-service](event_service_spec.md)、[media-service](media_service_spec.md)、[push-service](push_service_spec.md) | 相应服务的详细规格/实施任务书 |
| 领域规格 | [消防通道检测](fire_access_obstruction_detection_spec.md) | 算法/业务域专用的阶段说明；不等同于平台服务合同 |

`Analysis/PRD.md` 是产品需求和范围的唯一入口；`Research/RESEARCH.md` 保存会影响方案的外部证据与取舍；`Design/STRUCTURE.md` 记录高层结构；`Analysis/SPEC.md` 保存平台级跨服务技术合同。全局 SPEC 沿用仓库现有 `Analysis/` 路径，但在流程上属于结合 Research 和 Design 后形成的技术规格，不表示它先于这两个阶段。`Specification/<service>_service_spec.md` 保存相应服务的详细合同和实施阶段。现有服务文件多为“实现任务书”，其中描述的是目标合同和计划，不等于代码已实现；开始任务时必须对照源码核实。新变更包只描述增量，不复制整份 PRD 或全局 SPEC。修改完成后，应把长期有效的行为/接口合同回写到其唯一基线文档。

## 目录约定

```text
project/docs/Specification/
├── README.md
├── WORKFLOW.md
├── COMPARISON.md
├── assets/                         # 规格文档使用的图片或其他附件
├── templates/
│   ├── proposal.md
│   ├── spec.md
│   ├── design.md
│   └── tasks.md
├── changes/
│   ├── <YYYY-MM-DD>-<short-name>/   # 当前变更包；与实现放在同一任务分支
│   └── archive/
│       └── <YYYY-MM-DD>-<short-name>/ # 已完成变更的决策和验收记录
├── auth_service_spec.md
├── device_service_spec.md
├── event_service_spec.md
├── media_service_spec.md
├── push_service_spec.md
└── fire_access_obstruction_detection_spec.md # 领域专属规格
```

仓库已有目录名为 `Specification`（单数），并已有多份服务规格。为避免破坏链接和制造重复基线，沿用该目录名；“Specifications”指这一整组文档。`changes/` 按需创建；没有活跃变更时不必保留空目录。算法或领域专属规格（例如消防通道检测算法）继续按其实际责任边界维护，不自动并入平台服务合同。

## 变更状态

状态写在变更包 `proposal.md` 的元数据中：`探索中`、`待评审`、`可实现`、`实现中`、`待验收`、`已完成`、`已取消`。`可实现`只表示该变更包已通过适用的技术/范围评审、没有阻塞实现的未决项；它不代表用户或产品责任人已批准其中的业务决策。高风险或超出 PRD 的业务决策仍须由相应责任人明确确认，并在状态进入 `可实现` 前记录该决定。

一般状态流转为 `探索中 → 待评审 → 可实现 → 实现中 → 待验收 → 已完成`；评审要求修改时回到 `探索中` 并更新材料。轻量变更可跳过正式评审，材料具备且开始编码后进入 `实现中`。任何未完成阶段都可在记录原因后转为 `已取消`。原型结论不确定时保持 `探索中`，写清下一步验证；规格/设计/任务不再各自维护第二套生命周期状态。

## 使用方式

从 [工作流](WORKFLOW.md) 根据改动大小与风险选择路径。标准路径按 `proposal → spec → design → tasks → review → implement → verify → reconcile/archive` 推进。小改动允许压缩为一个轻量变更说明；缺陷使用缺陷字段；探索性原型可以先验证，但结论需回写成可检查的规格再进入正式实现。

本工作流是 Markdown 文档约定，不要求安装 Spec Kit、OpenSpec、Kiro 或 BMAD，也不要求迁移现有 Git 分支/PR 流程。

模板：[proposal.md](templates/proposal.md) · [spec.md](templates/spec.md) · [design.md](templates/design.md) · [tasks.md](templates/tasks.md)。

变更包目录的用途和归档步骤见 [changes/README.md](changes/README.md)。
