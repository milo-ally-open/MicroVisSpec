# 项目文档与开发工作流

平台级文档按 **Analysis → Research → Design → Specification** 形成依据链：Analysis 明确用户问题与范围，Research 为未决问题提供证据，Design 说明系统结构，Specification 给出可实现、可验收的行为与技术合同。各服务或领域组通过 `proposal → behavior spec → technical design → tasks` 组织长期合同和当前工作；较小需求可以精简不需要的文档阶段，但要说明原因；不得把假设当作用户已确认的需求。

单项功能、缺陷或跨服务工作的 Spec 流程、评审门槛和文档组维护规则见 [Specification 工作流](Specification/README.md)。其中 `Analysis/PRD.md` 是产品范围依据，`Analysis/SPEC.md` 是全局技术合同；`Specification/` 按服务和领域维护持久的规格组，Git 分支、提交和 PR 记录每次开发的变更历史。

`Analysis/SPEC.md` 沿用仓库现有路径，虽然文件位于 `Analysis/`，流程上仍是结合 Research 和 Design 后形成的全局技术规格；文件夹位置不表示它先于 Research 或 Design 阶段。
