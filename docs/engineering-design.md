# Knowledge Hub 工程设计

## Context

基于 `docs/knowledge-hub.md` 的产品设计，我们需要将其转化为可落地的工程方案。核心挑战在于：当前 agent 能力不支持"模糊概念触发行为"，因此需要通过 CLAUDE.md Rules 来编排 agent 与 Knowledge Hub 的交互流程。本方案是 MVP（本地单用户），核心验证 tag 系统 + 评论系统 + agent 集成流程的可行性。

---

## 1. 整体架构

```
Claude Code Session 1 --spawn--> MCP Shim 1 --gRPC--+
Claude Code Session 2 --spawn--> MCP Shim 2 --gRPC--+
                                                     |
                                                     v
                                  +--------------------------------+
                                  |  Knowledge Hub API Server      |
                                  |  (long-running service)        |
                                  |                                |
                                  |  +-------------------------+   |
                                  |  |  Service Layer          |   |
                                  |  |  (business logic)       |   |
                                  |  +------------+------------+   |
                                  |               |                |
                                  |  +------------v------------+   |
                                  |  |  SQLite (WAL mode)      |   |
                                  |  +-------------------------+   |
                                  +--------------------------------+
```

**两个独立进程：**

- **MCP Shim**：轻量 stdio 进程，Claude Code 每个 session spawn 一个。职责仅为 MCP 协议解析 + gRPC 转发，不含业务逻辑。
- **Knowledge Hub API Server**：长期运行的独立服务，通过 gRPC 暴露业务接口。管理 SQLite 存储，处理所有并发请求。

**为什么用 gRPC：**
两个独立进程之间需要网络通信。gRPC 提供强类型契约（proto）、高效序列化、代码生成，比手写 HTTP + JSON 更可靠。

**并发处理：**
所有请求汇聚到单一 API Server 进程，由 Go 标准并发原语处理。SQLite 使用 WAL 模式，支持并发读 + 串行写，对 MVP 的负载量级绰绰有余。

---

## 2. Agent 集成流程

通过 CLAUDE.md Rules 编排，Skill 定义具体操作步骤，MCP 提供 tool call 能力。

### 2.1 工作流程

```
Task Received
    |
    v
[Phase 1: Task Analysis]
    Agent evaluates task complexity + own capabilities
    |
    +-- Simple/Clear -----------------------------------+
    |                                                   |
    +-- Uncertain -- Ask user if search needed ---+     |
    |                                             |     |
    +-- Complex / Capability gap                  |     |
         |                                        |     |
         v                                        v     |
    [Phase 2: Knowledge Collection]                     |
    Spawn subagent to search Knowledge Hub              |
    Return relevant knowledge summary                   |
         |                                              |
         v                                              |
    [Phase 3: Integrate knowledge, adjust plan]         |
         |                                              |
         v                                              v
    [Phase 4: Execution]
    Execute task normally
    After each subtask: append notes to scratchpad.md
         |
         v
    [Phase 5: Task Complete]
    Inform user: "Task complete. If you'd like me to summarize
    learnings and contribute to Knowledge Hub, use /reflect."
         |
         v (user triggers /reflect)
    [Phase 6: Reflect] (user-triggered via /reflect skill)
    +-- Read scratchpad.md + current context
    +-- Comment on used knowledge (useful/not useful)
    +-- Identify new knowledge candidates
    +-- Present to user for confirmation (MVP)
    +-- Submit approved knowledge via kh_contribute
```

### 2.2 触发条件设计

**知识搜集触发（Phase 2）：**
- 明确能力缺口：需要某个 tool/skill 但手里没有
- 任务涉及不熟悉的领域/系统
- 用户主动要求搜索 knowledge hub

**Reflect 触发（Phase 6）——用户显式触发：**
- 用户输入 `/reflect` 命令（MVP 阶段唯一触发方式）
- Agent 在主任务完成后应主动提示用户可使用 `/reflect`
- 未来当模型具备原生内省循环能力时，可无缝切换为自动触发

### 2.3 Scratchpad 设计

文件路径：项目根目录 `memory/scratchpad.md`

Agent 在每个子任务完成后直接追加轻量笔记（不创建 subagent），格式：

```markdown
## Session: 2026-02-28T10:30:00

### [解决] API 超时问题
- tags: #debugging #timeout #api
- 发现 connection pool 配置不当导致超时
- 解决方案：调整 MaxIdleConns 从 2 到 10

### [发现] 公司 CI 需要特殊环境变量
- tags: #ci #env #onboarding
- CI_CUSTOM_TOKEN 需要在 pipeline 中手动设置
```

Reflect 阶段由 subagent 读取 scratchpad + 当前 context，进行深度整理和知识贡献。

---

## 3. 数据模型

### 3.1 Knowledge Entry

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| title | string | 标题，用于列表展示 |
| summary | string | 摘要，渐进式检索的第二层 |
| body | string | 正文（Markdown） |
| tags | []string | 标签列表 |
| weight | float64 | 权重分数，影响排序 |
| author | string | 贡献者标识 |
| status | enum | active / archived |
| created_at | timestamp | 创建时间 |
| updated_at | timestamp | 更新时间 |
| accessed_at | timestamp | 最后被读取的时间（用于检测长期未使用） |
| access_count | int | 被读取次数 |
| append_count | int | 追加次数（用于触发全文重构阈值） |
| needs_rewrite | bool | 是否需要全文重构（append_count 超过阈值时自动标记） |

### 3.2 Comment

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| knowledge_id | string | 关联知识条目 |
| type | enum | success / failure / supplement / correction |
| content | string | 评论内容 |
| reasoning | string | **必填**：为什么给出这个评论（证据、上下文） |
| scenario | string | 使用场景描述 |
| author | string | 评论者标识 |
| processed | bool | 是否已被整理 agent 处理（默认 false） |
| processed_at | timestamp | 处理时间 |
| created_at | timestamp | 创建时间 |

**评论类型说明：**
- `success`：知识有用，在场景中成功应用
- `failure`：知识无效（过时、场景不匹配、描述不清等）
- `supplement`：补充额外信息（新场景、额外注意事项等）
- `correction`：指出知识中的具体错误并给出修正

**评论质量要求（在 Skill 中约束）：**
- `reasoning` 字段必须说明 WHY，不能是空泛的"有用"/"没用"
- `correction` 类型必须在 content 中指出原文哪里错、正确内容是什么
- `supplement` 类型必须在 content 中提供具体的补充信息

### 3.3 Tag

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| name | string | 标准名称 |
| aliases | []string | 别名列表（归一化用） |
| frequency | int | 使用频次（知识条目中出现的次数） |

### 3.4 CurationLog（整理日志）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| action | enum | merge_supplement / apply_correction / downgrade / archive / merge_tags / merge_knowledge / resolve_conflict |
| target_id | string | 被操作的知识/tag ID |
| source_ids | []string | 触发此操作的评论/知识/tag IDs |
| description | string | 整理 agent 对此操作的说明 |
| diff | string | 变更内容的 diff（正文修改时记录） |
| created_at | timestamp | 操作时间 |
| agent_id | string | 执行整理的 agent 标识 |

### 3.5 ConflictReport（冲突文档）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| type | enum | correction_conflict / knowledge_conflict |
| knowledge_ids | []string | 涉及的知识条目 IDs |
| comment_ids | []string | 涉及的评论 IDs |
| description | string | Agent 对冲突的分析说明 |
| status | enum | open / resolved |
| resolution | string | 解决方案（resolved 后填写） |
| created_at | timestamp | 创建时间 |
| resolved_at | timestamp | 解决时间 |

### 3.6 权重计算

```
weight = base_weight
       + SUM(decay(success_comment.created_at) * 1.0)  -- success with time decay
       - (unprocessed_failure_comments) * 2.0
       + access_count * 0.1
       + is_onboarding * 10.0

where decay(t) = exp(-lambda * days_since(t))
      lambda = 0.01 (half-life ~69 days, configurable)
```

**时间衰减**：
- Success 评论的加分随时间指数衰减，确保持续被验证有效的知识保持高权重
- 早年高分但近期无人验证的知识会自然下降，新知识有机会浮现
- Failure 评论不衰减——一次失败信号始终保持警示作用，直到被处理

**驱逐阈值**（Failure Eviction）：
- 在滑动时间窗口（默认 30 天）内，如果一篇知识收到的 Failure + Correction 评论达到绝对阈值（默认 3 条），系统自动将其标记为待仲裁
- 不走渐进扣分，而是直接由 `kh_list_flagged` 返回，由管理员 Agent 决定：归档、重写、或创建冲突报告
- 这避免了高基础分知识的"马太效应"——即使历史评分很高，短期内集中出现的失败信号也能快速触发审查

**其他规则**：
- 只统计未处理的 Failure 评论对权重的惩罚
- 已处理的评论（补充已合并、纠错已应用）不再影响权重
- 访问量提供微弱的正向信号
- Onboarding 知识获得大幅提权

### 3.7 知识更新策略：追加优先 + 重构阈值

**核心问题**：要求 LLM 为一个小纠错在 Tool Call 中输出完整的几千字 Markdown 正文，极易触发 Output Token 截断，且经常破坏原有的代码块缩进或排版格式。

**解决方案**：

- **日常更新用追加**：`kh_append_knowledge` 在原文末尾以 Blockquote 形式追加带时间戳的记录，不触碰原文。
- **达到阈值才重构**：`append_count` 每次追加 +1，超过阈值（默认 5 次）时自动标记 `needs_rewrite = true`。
- **管理员 Agent 批量重构**：`kh_list_flagged` 返回 `needs_rewrite` 的条目，管理员 Agent 在巡检时进行一次全文融合与润色（此时调用 `kh_update_knowledge` 做全量替换），并重置 `append_count = 0`、`needs_rewrite = false`。

**追加格式示例**：

```markdown
> **[Supplement] 2026-02-28T14:30:00**
> 补充：在 K8s 环境下，还需要配置 graceful shutdown 的 preStop hook，
> 给 load balancer 足够时间摘除 pod。
```

这样日常的补充和纠错只需输出增量内容（几十到几百字），彻底规避了 LLM 长文本输出的不稳定性。全文重构作为低频后台任务，由管理员 Agent 在巡检时统一处理。

---

## 4. 后台知识整理流程

MVP 阶段通过用户手动触发（`/admin-inspect` skill），未来可自动化。

**核心原则：API Server 负责发现问题（算法），Agent 负责解决问题（语义理解）。**

### 4.1 Phase 1：逐条知识审查

```
Trigger Curation
    |
    v
[Step 1.1] API Server batch analysis (algorithmic)
    Call kh_list_flagged, returns knowledge entries needing review:
    +-- Entries with unprocessed comments (processed=false)
    +-- Entries with failure comment ratio > 50%
    +-- Entries with 30+ days of no access
    +-- Entries with needs_rewrite = true (append_count exceeded threshold)
    +-- Entries hitting failure eviction threshold (see 3.6)
    +-- Each entry includes: comment aggregation + current weight + flag reasons
    |
    v
[Step 1.2] Agent reviews each entry
    For each flagged knowledge:
    |
    +-- Call kh_get_review(id) to get full text + all unprocessed comments
    |
    +-- If needs_rewrite = true:
    |   +-- Read full text (including all appended blockquotes)
    |   +-- Perform full rewrite: merge all appendices into coherent body
    |   +-- Call kh_update_knowledge (full replacement, reset append_count)
    |
    +-- If hitting failure eviction threshold:
    |   +-- Assess whether knowledge is fundamentally flawed or just outdated
    |   +-- Flawed -> archive directly
    |   +-- Outdated but salvageable -> rewrite with updated info
    |   +-- Cannot determine -> create ConflictReport
    |
    +-- Process supplement comments:
    |   +-- Evaluate value and accuracy of supplementary content
    |   +-- Valuable -> call kh_append_knowledge (incremental, no full rewrite)
    |   +-- Not valuable -> mark as processed only
    |
    +-- Process correction comments:
    |   +-- Compare original text with correction
    |   +-- Correction valid -> call kh_append_knowledge with correction note
    |   +-- Correction invalid -> mark as processed only
    |   +-- Cannot determine -> create ConflictReport
    |
    +-- Process failure comments:
    |   +-- Analyze failure cause
    |   +-- Outdated -> append scope/version note or downgrade
    |   +-- Scenario mismatch -> append scope clarification
    |   +-- Unclear description -> append clarification note
    |
    +-- Call kh_mark_processed(comment_ids)
    |
    +-- Call kh_log_curation to write curation log
    |
    v
[Step 1.3] API Server batch weight recalculation (algorithmic)
    Call kh_recalculate_weights
```

### 4.2 Phase 2：全局 Tag 整理 + 知识去重

```
[Step 2.1] API Server analysis (algorithmic)
    Call kh_tag_health to get:
    +-- Suspected synonym tag pairs
    |   Detection logic (any of):
    |   +-- Edit distance <= 2 (logging vs loging)
    |   +-- One is substring of another (log vs logging)
    |   +-- Co-occurrence rate > 80% (two tags frequently appear together)
    |   +-- Existing alias mapping match
    +-- Low frequency tags (< 3 uses)
    +-- Abnormally high frequency tags (> 30% share, granularity may be too coarse)

    Call kh_find_similar to get:
    +-- Suspected duplicate knowledge pairs (tag overlap > 80%)
    +-- Each pair includes: overlapping tags, title comparison, creation time
    |
    v
[Step 2.2] Agent processes tags
    For each suspected synonym tag group:
    +-- Determine if truly synonymous
    +-- Choose which to keep as canonical name
    +-- Call kh_merge_tags(target, sources)
    +-- Call kh_log_curation to log
    |
    v
[Step 2.3] Agent processes suspected duplicate knowledge
    For each suspected duplicate pair:
    +-- Call kh_read_full to read both entries
    +-- Determine:
    |   +-- Exact duplicate -> merge, keep the more complete one
    |   +-- Partial overlap -> merge complementary content
    |   +-- Appears similar but actually different -> keep both, adjust tags
    |   +-- Conflicting (same problem, different solutions, cannot judge)
    |       -> Create ConflictReport
    +-- Call kh_merge_knowledge or skip
    +-- Call kh_log_curation to log
```

注：不再需要 rebuild_directory 步骤——Faceted 浏览基于实时查询，tag 合并和知识合并后自动反映在浏览结果中。

### 4.3 整理日志示例

```
[2026-02-28T14:30:00] action=apply_correction
  target: knowledge#a1b2c3 "Go HTTP Server 优雅关闭方法"
  source: comment#d4e5f6
  description: "评论指出原文中 Shutdown timeout 应为 30s 而非 5s（reasoning: 生产环境中
  长连接需要更多时间排空）。经确认修改正文第4段的超时参数。"
  diff: -ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        +ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

[2026-02-28T14:35:00] action=merge_tags
  target: tag "logging"
  sources: ["log", "日志"]
  description: "三个 tag 指向同一概念。'logging' 使用频率最高（23次），选为标准名。
  'log'（8次）和 '日志'（5次）作为别名。"
```

### 4.4 未来自动化路径

当积累足够的人工审批数据后：
1. 高置信度操作（编辑距离 ≤ 1 的 tag 合并）可自动执行
2. 中置信度操作自动生成待审批列表
3. 定期自动触发巡检（cron）

---

## 5. 完整 API 接口设计

### 5.1 API 分层

```
+-----------------------------------------+
|     MCP Tools (visible to Agent)        |
|                                         |
|  +---------------+  +----------------+  |
|  | Worker Agent  |  | Curation Agent |  |
|  | Tools         |  | Tools          |  |
|  +------+--------+  +-------+--------+  |
|         |                   |           |
|         +--------+----------+           |
|                  v                      |
|           gRPC Service                  |
|       (1:1 mapping to MCP Tools)        |
|                  |                      |
|                  v                      |
|           Service Layer                 |
|       (business logic + algorithms)     |
|                  |                      |
|                  v                      |
|            Store Layer                  |
|             (SQLite)                    |
+-----------------------------------------+
```

### 5.2 工作 Agent MCP Tools

供日常工作的 agent 使用，通过 search-knowledge / contribute-knowledge skill 编排。

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_browse` | selected_tags []string (可选) | {tags: [{name, count}], total_matches int} | Faceted 浏览：无参数返回顶层高频 tag；传入已选 tag 返回交叉过滤后的下一层 tag 供下钻 |
| `kh_search` | tags []string, keyword string | [{id, title, summary, tags, weight}] (最多20条) | 搜索知识，按权重排序，返回包含摘要（Top 5 摘要总计约千字 Token，当前模型可轻松承载） |
| `kh_read_full` | id string | {全部字段 + comments概要} | 读取全文（同时更新 accessed_at, access_count） |
| `kh_contribute` | title, summary, body, tags | {id} | 贡献新知识 |
| `kh_append_knowledge` | id, type (supplement/correction), content | {id, append_count} | 追加内容到知识末尾（Blockquote 格式带时间戳），避免全文替换 |
| `kh_comment` | knowledge_id, type, content, reasoning, scenario | {id} | 添加评论（reasoning 必填） |

### 5.3 整理 Agent MCP Tools

供知识整理 agent 使用，通过 admin-inspect skill 编排。

**发现问题（API Server 算法驱动）：**

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_list_flagged` | 无 | [{id, title, flag_reasons[], comment_stats}] | 列出需要审查的知识（有未处理评论、高失败率、长期未使用、needs_rewrite、failure eviction threshold） |
| `kh_tag_health` | 无 | {similar_pairs[], low_freq[], high_freq[]} | Tag 健康度报告 |
| `kh_find_similar` | 无 | [{id_a, title_a, id_b, title_b, overlap_tags[], overlap_ratio}] | 疑似重复知识对 |

**审查详情：**

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_get_review` | id string | {knowledge全文 + 所有未处理comments} | 获取知识审查详情 |

**执行操作：**

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_update_knowledge` | id, title?, summary?, body?, tags? | {updated_at} | 全量更新知识内容（仅用于管理员 Agent 全文重构，重置 append_count） |
| `kh_archive` | id string | {} | 归档（软删除）知识 |
| `kh_mark_processed` | comment_ids []string | {} | 标记评论为已处理 |
| `kh_merge_tags` | target string, sources []string | {affected_knowledge_count} | 合并 tag，所有 sources 的知识重新打标为 target |
| `kh_merge_knowledge` | target_id, source_ids, merged_body, merged_summary | {id} | 合并知识条目（source 归档，target 更新） |
| `kh_create_conflict` | type, knowledge_ids, comment_ids, description | {id} | 创建冲突报告 |
| `kh_log_curation` | action, target_id, source_ids, description, diff? | {id} | 写入整理日志 |

**系统操作：**

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_recalculate_weights` | 无 | {updated_count} | 批量重算所有知识权重 |

### 5.4 gRPC Service 定义

```protobuf
syntax = "proto3";
package knowledgehub.v1;

service KnowledgeHub {
  // ===== 工作 Agent 接口 =====

  // 渐进式检索（Browse -> Search(含摘要) -> GetFull，3步完成）
  rpc BrowseDirectory(BrowseDirectoryRequest) returns (BrowseDirectoryResponse);
  rpc Search(SearchRequest) returns (SearchResponse);  // 返回含 summary 的结果列表
  rpc GetFull(GetFullRequest) returns (GetFullResponse);

  // 知识贡献与追加
  rpc Contribute(ContributeRequest) returns (ContributeResponse);
  rpc AppendKnowledge(AppendKnowledgeRequest) returns (AppendKnowledgeResponse);

  // 评论
  rpc AddComment(AddCommentRequest) returns (AddCommentResponse);

  // ===== 整理 Agent 接口 =====

  // 发现问题（算法驱动）
  rpc ListFlagged(ListFlaggedRequest) returns (ListFlaggedResponse);
  rpc GetTagHealth(GetTagHealthRequest) returns (GetTagHealthResponse);
  rpc FindSimilarKnowledge(FindSimilarRequest) returns (FindSimilarResponse);

  // 审查详情
  rpc GetReview(GetReviewRequest) returns (GetReviewResponse);

  // 执行操作
  rpc UpdateKnowledge(UpdateKnowledgeRequest) returns (UpdateKnowledgeResponse);
  rpc ArchiveKnowledge(ArchiveKnowledgeRequest) returns (ArchiveKnowledgeResponse);
  rpc MarkCommentsProcessed(MarkProcessedRequest) returns (MarkProcessedResponse);
  rpc MergeTags(MergeTagsRequest) returns (MergeTagsResponse);
  rpc MergeKnowledge(MergeKnowledgeRequest) returns (MergeKnowledgeResponse);
  rpc CreateConflictReport(CreateConflictRequest) returns (CreateConflictResponse);
  rpc LogCuration(LogCurationRequest) returns (LogCurationResponse);

  // 系统操作
  rpc RecalculateWeights(RecalculateRequest) returns (RecalculateResponse);
}

// ===== 枚举 =====

enum CommentType {
  COMMENT_TYPE_UNSPECIFIED = 0;
  COMMENT_TYPE_SUCCESS = 1;
  COMMENT_TYPE_FAILURE = 2;
  COMMENT_TYPE_SUPPLEMENT = 3;
  COMMENT_TYPE_CORRECTION = 4;
}

enum CurationAction {
  CURATION_ACTION_UNSPECIFIED = 0;
  CURATION_ACTION_MERGE_SUPPLEMENT = 1;
  CURATION_ACTION_APPLY_CORRECTION = 2;
  CURATION_ACTION_DOWNGRADE = 3;
  CURATION_ACTION_ARCHIVE = 4;
  CURATION_ACTION_MERGE_TAGS = 5;
  CURATION_ACTION_MERGE_KNOWLEDGE = 6;
  CURATION_ACTION_CREATE_CONFLICT = 7;
}

enum KnowledgeStatus {
  KNOWLEDGE_STATUS_UNSPECIFIED = 0;
  KNOWLEDGE_STATUS_ACTIVE = 1;
  KNOWLEDGE_STATUS_ARCHIVED = 2;
}

enum ConflictStatus {
  CONFLICT_STATUS_UNSPECIFIED = 0;
  CONFLICT_STATUS_OPEN = 1;
  CONFLICT_STATUS_RESOLVED = 2;
}

// ===== 详细 Message 定义见 proto 文件 =====
```

### 5.5 渐进式检索 Token 控制

| 层级 | 返回内容 | 预估 Token |
|------|---------|-----------|
| 目录浏览 | tag 列表 + 每个 tag 文章数 + 可下钻提示 | ~200-500 |
| 搜索结果 | 标题 + 摘要 + tags + 权重（最多 20 条） | ~500-1500 |
| 全文 | 完整 Markdown + 评论概要 | ~500-2000 |

---

## 6. 动态 Faceted 浏览算法

**设计变更**：放弃预生成静态目录树。原方案中"一篇文章可出现在多个目录路径"会导致组合爆炸——若物理具象化所有 tag 组合的目录树，路径数量随 tag 数量指数增长。改为动态 faceted 浏览，按需实时计算。

```
kh_browse(selected_tags=[])

Input: selected_tags (already chosen tags for filtering, can be empty)
Output: {tags: [{name, count}], total_matches}

Algorithm:
1. If selected_tags is empty:
   - Count frequency of all tags across active knowledge entries
   - Return top N tags (configurable, default 15) with their article counts
2. If selected_tags is not empty:
   - Filter knowledge entries that contain ALL selected_tags
   - Among filtered entries, count frequency of remaining tags (excluding selected_tags)
   - Return top N remaining tags with their counts within the filtered set
   - Return total_matches = number of entries matching all selected_tags
3. Agent can iteratively drill down by adding tags to selected_tags
4. When total_matches is small enough (e.g. <= 10), Agent switches to kh_search
   with the accumulated tags to get titles + summaries
```

**示例交互**：
```
Agent: kh_browse()
  -> {tags: [{go, 45}, {debugging, 38}, {k8s, 25}, ...], total: 120}

Agent: kh_browse(selected_tags=["go"])
  -> {tags: [{concurrency, 12}, {http, 10}, {testing, 8}, ...], total: 45}

Agent: kh_browse(selected_tags=["go", "concurrency"])
  -> {tags: [{channel, 5}, {mutex, 4}, ...], total: 12}

Agent: kh_search(tags=["go", "concurrency"])
  -> [{id, title, summary, tags, weight}, ...] (12 results with summaries)
```

**优势**：
- 无需预计算或缓存目录树，零存储开销
- 支持任意 tag 组合的自由探索，不受预定义层级限制
- 后端仅需一次 SQL 聚合查询，计算资源极简

---

## 7. 项目结构

```
knowledge-hub/
├── cmd/
│   ├── server/
│   │   └── main.go              # 启动 Knowledge Hub API Server (gRPC)
│   └── mcp/
│       └── main.go              # 启动 MCP Shim (stdio, 连接 API Server)
├── internal/
│   ├── service/                 # 核心业务逻辑
│   │   ├── knowledge.go         # 知识 CRUD + 渐进式检索
│   │   ├── comment.go           # 评论管理
│   │   ├── browse.go            # Faceted 浏览算法
│   │   ├── weight.go            # 权重计算
│   │   ├── curation.go          # 整理相关：flagging、tag健康、相似检测
│   │   └── conflict.go          # 冲突报告管理
│   ├── mcp/                     # MCP 协议层
│   │   ├── server.go            # MCP server 初始化
│   │   └── tools.go             # Tool 定义 + handler（gRPC client 转发）
│   ├── grpc/                    # gRPC 服务层
│   │   └── server.go            # gRPC server，调用 service 层
│   ├── store/                   # 存储层
│   │   ├── sqlite.go            # SQLite 连接管理 (WAL 模式)
│   │   ├── knowledge.go         # Knowledge 表
│   │   ├── tag.go               # Tag 表
│   │   ├── comment.go           # Comment 表
│   │   ├── curation_log.go      # CurationLog 表
│   │   └── conflict.go          # ConflictReport 表
│   └── domain/                  # 领域模型
│       ├── knowledge.go
│       ├── tag.go
│       ├── comment.go
│       ├── browse.go
│       ├── curation.go
│       └── conflict.go
├── proto/                       # gRPC protobuf 定义
│   └── knowledgehub/
│       └── v1/
│           └── knowledgehub.proto
├── rules/                       # CLAUDE.md Rules
│   └── knowledge-hub.md
├── skills/                      # Skill 定义
│   ├── search-knowledge.md
│   ├── contribute-knowledge.md
│   ├── reflect.md
│   └── admin-inspect.md
├── docs/
│   ├── knowledge-hub.md         # 产品设计文档（已有）
│   └── engineering-design.md    # 本文档
├── go.mod
└── go.sum
```

---

## 8. 关键依赖

| 依赖 | 用途 |
|------|------|
| `github.com/modelcontextprotocol/go-sdk` | 官方 MCP Go SDK (stdio transport) |
| `google.golang.org/grpc` | gRPC 框架 |
| `google.golang.org/protobuf` | Protobuf 序列化 |
| `modernc.org/sqlite` | 纯 Go SQLite（无 CGO 依赖） |
| `github.com/google/uuid` | UUID 生成 |
| `github.com/agnivade/levenshtein` | 编辑距离计算（tag 同义检测） |

---

## 9. Rules 与 Skill 设计

### 9.1 Rule (写入 CLAUDE.md)

```
你的环境中配置了 Knowledge Hub —— 一个 agent 知识共享平台。
当你遇到复杂任务、不确定的技术问题、或发现自己缺少某种能力时，
可以通过 kh_browse / kh_search 搜索已有经验。
当你解决了有价值的问题后，在 memory/scratchpad.md 中记录要点。
当阶段性工作完成时，提示用户可使用 /reflect 命令来总结经验并贡献到 Knowledge Hub。
```

### 9.2 Skill: search-knowledge

```
1. 明确你要搜索的主题，提取关键词和可能的 tags
2. 调用 kh_browse 浏览目录结构，了解知识库的覆盖范围（可传入已知 tags 下钻）
3. 调用 kh_search 搜索相关知识（返回包含标题、摘要、tags、权重）
4. 根据搜索结果中的摘要判断相关性，挑选最相关的条目
5. 调用 kh_read_full 获取全文
6. 整合获得的知识，应用到当前任务
```

### 9.3 Skill: contribute-knowledge

```
1. 回顾 memory/scratchpad.md 中记录的经验
2. 对每条有价值的经验：
   a. 提炼标题（简洁、可搜索、< 80字符）
   b. 撰写摘要（200字以内，说明问题和解决方案）
   c. 撰写正文（完整的上下文、步骤、注意事项，Markdown）
   d. 标注 tags（3-5个，具体且有意义）
3. 展示给用户确认 (MVP 阶段)
4. 用户确认后调用 kh_contribute 提交
5. 如果使用了 Knowledge Hub 中的知识，调用 kh_comment 反馈使用结果
   - type: success / failure / supplement / correction
   - reasoning: 必须说明为什么给出这个评价（你的场景、证据）
   - scenario: 你在做什么任务
6. 如果有具体的补充或纠错内容，使用 kh_append_knowledge 追加到原知识
   - 追加内容会以 Blockquote 格式附加在原文末尾，不破坏原文
   - 不要尝试用 kh_update_knowledge 做小修改（那是管理员全文重构用的）
```

### 9.4 Skill: reflect（经验总结与知识贡献）

```
1. 读取 memory/scratchpad.md，回顾本次工作中记录的所有笔记
2. 结合当前 context，回顾本次使用了哪些 Knowledge Hub 中的知识
3. 对使用过的知识进行反馈：
   a. 调用 kh_comment 评价知识的有效性（success/failure）
   b. 如有补充或纠错，调用 kh_append_knowledge 追加内容
4. 从 scratchpad 中识别可贡献的新知识候选：
   a. 解决了之前未有记录的技术问题
   b. 发现了意外的坑或 workaround
   c. 积累了可复用的经验
5. 对每条候选知识，按 contribute-knowledge skill 流程整理并展示给用户
6. 用户确认后提交
7. 清理已处理的 scratchpad 条目
```

### 9.5 Skill: admin-inspect（知识整理）

```
## Phase 1：逐条审查

1. 调用 kh_list_flagged 获取需要审查的知识列表
2. 对每条 flagged 知识：
   a. 调用 kh_get_review(id) 获取全文和未处理评论
   b. 如果 needs_rewrite = true：
      - 读取全文（含所有追加的 blockquote）
      - 执行全文融合重写，调用 kh_update_knowledge（重置 append_count）
   c. 如果触发了 failure eviction threshold：
      - 评估知识是根本性错误还是仅过时
      - 错误 → 直接归档；过时但可修复 → 重写；无法判断 → 创建冲突报告
   d. 处理每条评论：
      - supplement: 评估价值，有价值则 kh_append_knowledge 追加
      - correction: 对比原文和纠错，成立则 kh_append_knowledge 追加纠错说明，存疑则创建冲突报告
      - failure: 分析原因，过时则追加版本说明或降级，描述不清则追加澄清
      - success: 无需特殊处理
   e. 调用 kh_mark_processed 标记已处理的评论
   f. 调用 kh_log_curation 记录每个操作及原因
3. 调用 kh_recalculate_weights 重算权重

## Phase 2：全局整理

4. 调用 kh_tag_health 获取 tag 健康报告
5. 对疑似同义 tag：判断 → 合并 → 记录日志
6. 调用 kh_find_similar 获取疑似重复知识
7. 对每对疑似重复：读取全文 → 判断 → 合并或创建冲突报告 → 记录日志

## 输出

- 整理完成后汇总本次操作：处理了多少评论、合并了多少 tag、合并/归档了多少知识
- 如有冲突报告，列出供人工排查
```

---

## 10. 实施步骤

### Phase 1: 基础框架 + gRPC
1. 初始化 Go 项目，配置依赖
2. 编写 proto 定义，生成 gRPC 代码
3. 定义领域模型 (`internal/domain/`)
4. 实现 SQLite 存储层 (`internal/store/`)，WAL 模式
5. 实现核心业务逻辑 (`internal/service/`)
6. 实现 gRPC 服务层 (`internal/grpc/`)
7. 实现 API Server main.go

### Phase 2: MCP Shim
8. 实现 MCP Shim (`internal/mcp/`)，gRPC client 连接 API Server
9. 实现 MCP Shim main.go
10. 端到端测试：通过 Claude Code 调用工作 Agent MCP tools

### Phase 3: Agent 集成
11. 编写 Rules 和 Skill 定义
12. 配置 MCP Server 到 Claude Code
13. 端到端验证工作流程（搜索 → 贡献 → 评论）

### Phase 4: 知识整理功能
14. 实现 flagging 逻辑（未处理评论、高失败率、长期未使用）
15. 实现 tag 健康检查 + 同义检测
16. 实现知识相似度检测
17. 实现整理 Agent MCP tools
18. 实现整理日志 + 冲突报告
19. 编写 admin-inspect skill
20. 端到端验证整理流程

### Phase 5: Faceted 浏览 + 权重
21. 实现 Faceted 浏览算法（动态 tag 聚合查询）
22. 实现权重计算逻辑（含时间衰减 + 驱逐阈值）
23. 验证搜索排序效果

---

## 11. 验证方案

1. **搜索场景**: 手动录入几条知识 → 通过 Claude Code 使用 kh_browse + kh_search + kh_read 找到知识
2. **贡献场景**: 执行一个任务 → scratchpad 记录 → reflect 阶段通过 kh_contribute 贡献知识
3. **评论场景**: 使用一条知识 → 通过 kh_comment 反馈结果（含 reasoning）→ 验证权重变化
4. **整理场景（Phase 1）**: 添加 supplement/correction 评论 → 触发 admin-inspect → 验证知识更新 + 整理日志
5. **整理场景（Phase 2）**: 贡献多条含相似 tag 的知识 → 触发 admin-inspect → 验证同义检测和合并
6. **冲突场景**: 添加矛盾的 correction 评论 → 触发 admin-inspect → 验证冲突报告生成
7. **浏览场景**: 贡献多条不同 tag 的知识 → 通过 kh_browse 逐层下钻 → 验证 faceted 浏览结果正确
8. **完整流程**: 从任务开始到结束，走通 分析 → 搜集 → 执行 → reflect 全流程

---

## 12. 已知风险与妥协

| 风险 | 妥协方案 |
|------|---------|
| CLAUDE.md Rules 遵守率不可控（可能 < 70%） | MVP 先验证流程可行性，后续可考虑 hooks 机制加强 |
| Agent 不擅长识别 unknown unknowns | 搜索触发条件保持宽泛，宁可多搜也不漏搜 |
| Reflect 阶段 context 可能已被压缩 | Scratchpad 增量捕获弥补，reflect 时 subagent 基于 scratchpad 整理 |
| Tag 同义检测准确率有限 | 编辑距离+共现率双重验证；agent 给出 reasoning 后自动执行 |
| 整理 agent 误操作（错误合并/纠错） | 整理日志记录所有操作和原因，支持事后审计；冲突情况兜底给人工 |
| 知识库增长后整理 agent 成本上升 | API Server 负责 flagging，agent 只处理被标记的条目，成本与问题数正比 |
