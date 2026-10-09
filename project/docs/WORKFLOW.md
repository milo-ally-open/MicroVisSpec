# 项目文档与开发工作流

平台级文档按 **Analysis → Research → Design → Specification** 形成依据链：Analysis 明确用户问题与范围，Research 为未决问题提供证据，Design 说明系统结构，Specification 给出可实现、可验收的行为与技术合同。单项变更包按 `proposal → behavior spec → technical design → tasks` 组织，作为平台文档的增量输入；两种顺序服务于不同层级。需求规模较小时可以跳过不必要的阶段，但要记录跳过原因；不得把假设当作用户已确认的需求。

单项功能、缺陷或跨服务变更的 Spec 工作流、变更包结构、评审门槛和归档规则见 [Specification 工作流](Specification/README.md)。其中 `Analysis/PRD.md` 是产品范围依据，`Analysis/SPEC.md` 是全局技术合同；`Specification/` 存放按服务拆分的详细规格，以及每次代码变更的短期增量规格。

`Analysis/SPEC.md` 沿用仓库现有路径，虽然文件位于 `Analysis/`，流程上仍是结合 Research 和 Design 后形成的全局技术规格；文件夹位置不表示它先于 Research 或 Design 阶段。
