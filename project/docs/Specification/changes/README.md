# 变更包

每个当前开发中的增量规格放在 `changes/<YYYY-MM-DD>-<slug>/`，与功能代码一起评审。这里记录一次变更的缘由、期望行为、技术方案和实施状态，不作为系统当前状态的长期真相源。

实现通过验收后，先回写 `project/docs/Analysis/PRD.md`、`Research/RESEARCH.md`、`Design/STRUCTURE.md`、`Analysis/SPEC.md` 或相关服务规格中适用的长期内容，再把完整变更包移动到 `changes/archive/<YYYY-MM-DD>-<slug>/`。原型决定不继续或功能取消时，记录理由后以取消状态归档；结论不确定的探索仍保持开放并注明下一步。未交付的行为不能合入当前基线。

变更包模板和状态门槛见[工作流](../WORKFLOW.md)。
