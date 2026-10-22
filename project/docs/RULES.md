# 团队协作规范

## Git 工作流

1. **保护 `main`**：`main` 保持可用、可交付；日常开发在任务分支进行，通过 Pull Request 合并，不直接向 `main` 推送。
2. **按任务建分支**：一个分支聚焦一项任务，使用小写短名称，例如 `feature/event-alert`、`fix/stream-reconnect`、`docs/readme`。
3. **开始前同步**：从最新的 `main` 创建任务分支：

   ```bash
   git checkout main
   git pull --ff-only origin main
   git checkout -b feature/<task-name>
   ```

4. **保持提交清晰**：每个提交围绕一个明确改动，提交信息使用简短动词描述；可采用 `feat:`、`fix:`、`docs:`、`refactor:`、`test:`、`chore:` 等前缀。
5. **推送并发起 PR**：推送任务分支并创建 Pull Request，说明改动目的、主要内容和验证结果；合并前检查差异，并运行与改动相关的验证。
6. **合并后清理**：合并后同步本地 `main`，删除已完成的本地和远端任务分支。

## 提交前检查

- 用 `git status` 和 `git diff` 确认只包含本任务相关改动。
- 配置、令牌和其他敏感信息不得提交；本机专用文件应加入 `.gitignore`。
- 若无法运行相关验证，在 Pull Request 中说明原因和未验证范围。
