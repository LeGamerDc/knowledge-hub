# Skill: Contribute Knowledge

用于将工作过程中积累的经验整理后贡献到 Knowledge Hub。

## 步骤

1. **回顾 scratchpad**：读取 `memory/scratchpad.md` 中本次任务的记录，识别有价值的经验候选。

2. **筛选贡献候选**：选择满足以下条件之一的经验：
   - 解决了之前没有记录的技术问题
   - 发现了意外的坑或 workaround
   - 积累了可被其他 Agent 复用的配置/流程

3. **整理每条知识**：
   - **标题**：简洁可搜索，< 80 字符（示例："Go HTTP Server 优雅关闭：Shutdown timeout 应设为 30s"）
   - **摘要**：≤ 200 字，说明问题背景和核心解法
   - **正文**：完整的上下文、步骤、注意事项，Markdown 格式
   - **Tags**：3-5 个，具体且有意义（如 `go`、`http`、`timeout`，避免泛化的 `backend`）

4. **展示给用户确认**（MVP 阶段必须）：列出将提交的内容，等待用户 `y/N`。

5. **提交知识**：用户确认后调用 `kh_contribute` 提交。

6. **反馈使用过的知识**（若本次任务使用了 Knowledge Hub 中的知识）：
   - 调用 `kh_comment` 添加反馈：
     - `type`：`success`（有用）/ `failure`（无效/过时）/ `supplement`（有补充）/ `correction`（有纠错）
     - `reasoning`（必填）：说明 **为什么**——你的场景、证据、具体效果
     - `scenario`：你在做什么任务
   - 若有具体的小补充或纠错，调用 `kh_append_knowledge` 追加（Blockquote 格式，不破坏原文）
   - **不要**用 `kh_update_knowledge` 做小修改，那是管理员全文重构用的

## 注意事项

- 不确定是否值得贡献？宁可贡献，让 Hub 的评论系统来过滤。
- 正文中的代码块、命令、配置项务必精确，错误的知识比没有知识更有害。
- 若同一主题在 Hub 中已有类似条目，优先用 `kh_append_knowledge` 补充而不是新建。
