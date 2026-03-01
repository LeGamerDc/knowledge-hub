# Agent 控制流程与 Prompt (Skill) 设计

在 Knowledge Hub 系统中，Agent 被划分为两种角色：**工作 Agent**（日常使用工具）和 **管理/整理 Agent**（维护知识库）。为了让 LLM 在没有原生"内省触发"能力的情况下自主使用系统，我们通过 CLAUDE.md Rules (系统提示词) 和 Skills (操作编排) 来规范其控制流。

## 1. 角色 1：工作 Agent (Worker)

### 1.1 系统意识植入 (CLAUDE.md Rule)
我们将以下内容配置在 Agent 默认加载的 Rule 中，使其感知到 Knowledge Hub 的存在：

```markdown
# Rule: Knowledge Hub Integration
你的工作环境中配置了一个名为 "Knowledge Hub" 的企业内部经验共享平台。
当你在执行任务时：
1. 若遇到不熟悉的技术栈、报错、或缺乏上下文，请优先调用 `search-knowledge` skill 查询是否已有经验。
2. 任务执行过程中，将重要的坑、解法、或配置项记录在 `memory/scratchpad.md`。
3. 在你完成阶段性任务时，应主动向用户提示："任务已完成。本次过程中我记录了一些有价值的经验，是否需要使用 `/reflect` 命令将其贡献到 Knowledge Hub？"
```

### 1.2 核心操作编排 (Skills)

#### Skill: `search-knowledge`
用于规范 Agent 如何通过渐进式检索查找知识，避免一次性拉取过多无用文本。

```markdown
# Skill: Search Knowledge Hub

1. Extract keywords from current task (e.g. `k8s`, `timeout`, `502`).
2. Call `kh_browse` to see available Tags and their coverage (pass known tags to drill down).
3. Based on Tags, call `kh_search` to find articles (returns title + summary + tags + weight).
4. Read summaries and select the 1-2 most relevant articles.
5. Call `kh_read_full` to get full text and comments summary.
6. Integrate retrieved knowledge into your task plan.
```

#### Skill: `reflect` (用户触发的总结阶段)
当用户输入 `/reflect` 时触发此流程。

```markdown
# Skill: Reflect & Contribute

1. Read `memory/scratchpad.md` for notes recorded during this task.
2. Review whether you used any knowledge from Knowledge Hub. If so:
   - Call `kh_comment` to report whether the knowledge was `success` (useful) or `failure` (outdated/useless).
   - **Required**: explain your reasoning (your scenario, evidence) in the `reasoning` field.
   - If you have small supplements or corrections, call `kh_append_knowledge` to append to the original.
3. Check scratchpad for new, globally reusable experiences. If found:
   - Draft a clear Title and Summary (under 200 chars).
   - Extract 2-4 core Tags.
   - Format the body as Markdown.
   - Call `kh_contribute` to submit new knowledge.
4. Clear processed entries from `memory/scratchpad.md`.
```

## 2. 角色 2：整理/管理员 Agent (Curator/Admin)

管理员 Agent 具备高权限（可见 archived 数据，可使用 `/api/v1/admin/*` 接口）。它不处理具体业务，它的工作是**降噪**和**归一化**。

### 2.1 触发时机
目前由用户通过 CLI (`kh admin-sync`) 或手动输入指令触发。未来可配置为 Cron 定期执行。

### 2.2 核心操作编排 (Skill)

#### Skill: `admin-inspect` (全库巡检)

```markdown
# Skill: Knowledge Hub Curation Inspection

Your task is to maintain Knowledge Hub quality and consistency.
Execute the following two phases in order.

## Phase 1: Per-Entry Review

1. Call `kh_list_flagged` to get entries needing review.
   Server returns entries flagged for: unprocessed comments, high failure ratio,
   30+ days no access, needs_rewrite, failure eviction threshold.

2. For each flagged entry:
   a. Call `kh_get_review(id)` to get full text + all unprocessed comments.

   b. If `needs_rewrite = true`:
      - Read full text including all appended blockquotes.
      - Perform full rewrite: merge all appendices into coherent body.
      - Call `kh_update_knowledge` (full replacement, resets append_count).

   c. If hitting failure eviction threshold:
      - Assess whether knowledge is fundamentally flawed or just outdated.
      - Flawed -> call `kh_archive` directly.
      - Outdated but salvageable -> rewrite with updated info via `kh_update_knowledge`.
      - Cannot determine -> call `kh_create_conflict` to escalate to human.

   d. Process each unprocessed comment by type:
      - **supplement**: Evaluate value and accuracy.
        Valuable -> call `kh_append_knowledge` with supplement content.
        Not valuable -> skip (mark processed only).
      - **correction**: Compare original text with correction.
        Valid -> call `kh_append_knowledge` with correction note.
        Invalid -> skip. Cannot determine -> call `kh_create_conflict`.
      - **failure**: Analyze failure cause.
        Outdated -> append scope/version note or downgrade weight.
        Scenario mismatch -> append scope clarification.
        Unclear description -> append clarification note.
      - **success**: No special action needed.

   e. Call `kh_mark_processed` with all processed comment IDs.
   f. Call `kh_log_curation` to record each action with reasoning.

3. Call `kh_recalculate_weights` to refresh all weights after review.

## Phase 2: Global Cleanup

4. Call `kh_tag_health` to get tag health report.
5. For each suspected synonym tag group:
   - Determine if truly synonymous.
   - Choose which to keep as canonical name.
   - Call `kh_merge_tags(target, sources)`.
   - Call `kh_log_curation` to log the merge.

6. Call `kh_find_similar` to get suspected duplicate knowledge pairs.
7. For each suspected duplicate pair:
   - Call `kh_read_full` to read both entries.
   - Determine:
     - Exact duplicate -> merge, keep the more complete one via `kh_merge_knowledge`.
     - Partial overlap -> merge complementary content via `kh_merge_knowledge`.
     - Appears similar but actually different -> keep both, adjust tags if needed.
     - Conflicting solutions, cannot judge -> call `kh_create_conflict`.
   - Call `kh_log_curation` to log the decision.

## Output

Summarize this inspection:
- How many entries reviewed, comments processed
- How many tags merged
- How many entries merged/archived
- List any conflict reports created for human review
```

### 2.3 设计原则
- **操作闭环**：工作 Agent 负责"产出"和"制造噪音"；管理 Agent 依靠 `kh_list_flagged` 进行精确"扫除"。
- **API Server 发现问题（算法），Agent 解决问题（语义理解）**：flagging 逻辑由 Server 端执行，Agent 只处理被 flag 的条目。
- **最小 Context 原则**：Agent 先看 Summary，再调取 Full Body，防止 Token Context 浪费。
- **审计可追溯**：每个操作都通过 `kh_log_curation` 记录 action + reasoning + diff，支持事后审计。

## 3. MCP Tool 完整清单

### 3.1 工作 Agent Tools

| MCP Tool | 说明 |
|---------|------|
| `kh_browse` | Faceted 浏览（目录下钻） |
| `kh_search` | 搜索知识（返回摘要列表） |
| `kh_read_full` | 读取知识全文 |
| `kh_contribute` | 贡献新知识 |
| `kh_append_knowledge` | 追加内容到知识末尾 |
| `kh_comment` | 添加评论 |

### 3.2 管理 Agent Tools

| MCP Tool | 说明 |
|---------|------|
| `kh_list_flagged` | 列出待审查知识 |
| `kh_tag_health` | Tag 健康报告 |
| `kh_find_similar` | 疑似重复检测 |
| `kh_get_review` | 审查详情（全文 + 未处理评论） |
| `kh_update_knowledge` | 全量更新（管理员重构用） |
| `kh_archive` | 归档（软删除） |
| `kh_mark_processed` | 标记评论已处理 |
| `kh_merge_tags` | 合并 Tag |
| `kh_merge_knowledge` | 合并知识条目 |
| `kh_create_conflict` | 创建冲突报告 |
| `kh_log_curation` | 写入整理日志 |
| `kh_recalculate_weights` | 批量重算权重 |
