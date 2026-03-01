# Stage 7: Agent 集成 + 端到端验收

> 目标：编写 Rule/Skill 定义，配置 MCP Server，完成 5 个验收场景的端到端验证。

## 前置条件

- Stage 5 完成（MCP Shim 可用）
- Stage 6 完成（CLI 可用，便于验收数据检查）

## 任务清单

### 7.1 Rule 定义

- [ ] 创建 `rules/knowledge-hub.md`（工作 Agent Rule）
- [ ] 内容：告知 Agent Knowledge Hub 的存在 + 搜索/记录/提示 reflect 的行为引导
- [ ] 保持简洁（准则：一个人被告知"公司有个知识库"就够了）

### 7.2 Skill 定义

- [ ] 创建 `skills/search-knowledge.md`：
  - 提取关键词 → kh_browse 浏览 → kh_search 搜索 → kh_read_full 读全文 → 整合
- [ ] 创建 `skills/contribute-knowledge.md`：
  - 回顾 scratchpad → 提炼标题/摘要/正文/tags → 展示确认 → kh_contribute 提交 → kh_comment 反馈
- [ ] 创建 `skills/reflect.md`：
  - 读取 scratchpad → 回顾使用过的知识 → kh_comment 评价 → kh_append_knowledge 追加 → 识别新知识候选 → kh_contribute 提交 → 清理 scratchpad
- [ ] 创建 `skills/admin-inspect.md`：
  - Phase 1：kh_list_flagged → 逐条审查 → 处理评论 → kh_mark_processed → kh_log_curation → kh_recalculate_weights
  - Phase 2：kh_tag_health → 合并同义 tag → kh_find_similar → 合并/冲突报告 → 汇总输出

### 7.3 MCP 配置

- [ ] 编写 Claude Code MCP 配置（工作 Agent 配置：6 个 tool）
- [ ] 编写 Claude Code MCP 配置（管理 Agent 配置：12 个 tool）
- [ ] 启动脚本 / 使用说明

### 7.4 初始知识种子

- [ ] 准备 3-5 条种子知识（onboarding 级别）
- [ ] 通过 CLI 或直接 API 录入
- [ ] 验证种子知识在搜索中可发现

### 7.5 端到端验收测试

5 个核心验收场景（对应 `docs/knowledge-hub.md` §4.3）：

- [ ] **搜索场景**：Agent 通过 kh_browse + kh_search + kh_read_full 找到种子知识并使用
- [ ] **贡献场景**：Agent 完成任务后，通过 /reflect 贡献新知识到 Hub
- [ ] **评论场景**：Agent 使用知识后，通过 kh_comment 添加反馈 → 验证权重变化
- [ ] **治理场景**：管理员 Agent 执行 /admin-inspect → 完成 tag 归一化或知识审查
- [ ] **目录场景**：通过 kh_browse 逐层下钻 → 验证 Faceted 浏览结果正确

### 7.6 完整工作流验证

- [ ] 完整走通：任务分析 → 知识搜集 → 执行任务 → scratchpad 记录 → /reflect → 贡献知识
- [ ] 验证 Rule 的引导效果（Agent 是否主动搜索/记录/提示 reflect）

## 交付物

- `rules/knowledge-hub.md`
- `skills/search-knowledge.md`
- `skills/contribute-knowledge.md`
- `skills/reflect.md`
- `skills/admin-inspect.md`
- MCP 配置文件
- 5 个验收场景全部通过
- 完整工作流可用

## 参考文档

- `docs/engineering-design.md` §9 — Rules 与 Skill 设计
- `docs/specs/agent-workflows.md` — Agent 控制流程
- `docs/knowledge-hub.md` §4.3 — 验收检查点
- `docs/engineering-design.md` §11 — 验证方案
