# 技术设计：云哨 Spec 工作流 Skill

> 关联规格：[spec.md](spec.md)<br>
> 本文档组状态见 [proposal.md](proposal.md)。

## 当前实现与目标

- 当前 Skill 指令能引用项目约定，但没有可执行的领域组脚手架或跨文件追踪检查。
- 目标文件位于 `codearts_proof/skills/project-spec-workflow/`，使用项目模板初始化至 `project/docs/Specification/DOMAIN/`。
- 不修改已存在的服务组；Git 负责每次修改的历史。

## 方案

- `init_spec_group.py` 校验领域名、仓库根目录和模板，支持 light、standard、high-risk、bug、prototype 五种文档集合；先检查目标不存在，再从模板创建文件。
- `validate_spec_group.py` 使用 Python 标准库检查文档存在性、未完成占位符、REQ/AC/T 定义与引用，并返回适合自动化调用的退出码。
- Skill 本文保持流程和边界简洁；项目细节以 `project/docs/Specification/WORKFLOW.md` 及模板为准。
- 两个脚本不联网、不执行 Git 写操作，也不读取项目秘密。

## 接口和数据

- 初始化接口：`python scripts/init_spec_group.py --root ROOT --name DOMAIN --title TITLE --mode MODE`，目标位于 `project/docs/Specification/DOMAIN/`。
- 验证接口：`python scripts/validate_spec_group.py GROUP_PATH`。
- 所有输入输出为 UTF-8 Markdown；运行依赖 Python 3 标准库。

## 安全、运维与兼容

- 初始化器对存在的目标目录拒绝写入。
- 验证器只读目标 Markdown，不执行文档内容。
- 结构验证通过不代表文档审批或程序行为验收通过。
- 模板 ID 格式变化时要同步维护验证器。

## 备选方案和取舍

| 方案 | 优点 | 放弃原因/代价 |
|---|---|---|
| 只靠 Skill 自然语言 | 无脚本维护 | 目录创建与 ID 对照容易漏检 |
| 引入完整 Spec CLI | 流程自动化程度高 | 增加依赖、命令体系和项目文档重复 |
| Python 标准库脚本加 Markdown | 易检查、无外部依赖、适配现有目录 | 语义判断仍由评审完成 |

## 规格映射

实现落点与验收映射见 [spec.md](spec.md) 的需求追踪表。

## 未决项

无。

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-10 | 初始化实现设计 |
