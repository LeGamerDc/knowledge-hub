# Knowledge Hub 工程设计

## Context

基于 `docs/knowledge-hub.md` 的产品设计，我们需要将其转化为可落地的工程方案。本方案是 MVP（本地单用户），核心验证 tag 系统 + 评论系统 + agent 集成流程的可行性。通过 CLAUDE.md Rule 编排 agent 工作流程，MCP 提供 tools + resources。

### 设计变更记录

| 变更 | 原方案 | 现方案 | 理由 |
|------|-------|-------|------|
| 进程间通信协议 | gRPC + Protobuf | OpenAPI 3.0 + HTTP/JSON | MVP 阶段优先调试友好性：HTTP 可直接 curl 调试；oapi-codegen 提供等价的强类型代码生成（Server Handler + Client）；省去 protoc + gRPC 插件工具链 |
| 进程模型 | MCP Shim（stdio）+ API Server（HTTP）双进程 | 单进程 Knowledge Hub Server（HTTP MCP + REST API） | MCP 协议已支持 Streamable HTTP，Agent 可直连，Shim 的协议转换层不再必要 |
| 存储架构 | SQLite 承担所有查询（复杂索引 + SQL） | 内存索引（筛选/排序）+ SQLite 文档存储（持久化） | MVP 数据量级（千级条目）无需数据库查询引擎，内存操作更简单高效 |
| 权重计算 | 多因素公式（success/failure/access/onboarding） | 仅正向评论时间衰减 + 新知识初始权重 | 简化公式，failure 信号交由整理 Agent 处理而非影响排序 |
| Agent 交付层次 | Rule + Skill + MCP 三层 | Rule + MCP（tools + resources）两层 | Skill 层被 MCP Resources 天然替代，减少概念层和维护成本 |

---

## 1. 整体架构

### 1.1 进程模型

```
Agent 1 ----HTTP MCP----+
Agent 2 ----HTTP MCP----+----> Knowledge Hub Server
CLI Tool ---REST API----+      (MCP + REST + In-Memory Index + SQLite)
```

**单进程架构：**

Knowledge Hub Server 是唯一进程，同时提供：

- **MCP Server（Streamable HTTP）**：Agent 通过 HTTP MCP 直连，获取 tools 和 resources
- **REST API**：CLI 工具使用（系统管理、硬删除/恢复等）
- **In-Memory Index**：所有 active entry 的核心字段常驻内存，支持快速筛选和排序
- **SQLite（WAL mode）**：持久化存储，按 document 读写

**为什么不需要 MCP Shim：**
原方案中 MCP Shim 是 stdio 进程，每个 session spawn 一个，职责仅为协议转换 + HTTP 转发。MCP 协议已支持 Streamable HTTP transport，Go SDK 原生支持。Agent 可直连 HTTP MCP endpoint，消除了中间层。

### 1.2 存储架构

```
+---------------------+       +---------------------+
|   In-Memory Index   |       |   SQLite (WAL)      |
|                     |       |                     |
|  - id               |       |  knowledge table    |
|  - title            |  <->  |    (full document)  |
|  - summary          |       |                     |
|  - tags             |       |  comments table     |
|  - weight           |       |    (per knowledge)  |
|  - status           |       |                     |
|  - created_at       |       |  tags / curation /  |
+---------------------+       |  conflicts tables   |
                              +---------------------+
```

**内存索引**：所有 active knowledge entry 的 `id / title / summary / tags / weight / status / created_at` 载入内存。浏览（tag 聚合）、搜索（tag 匹配 + 关键词匹配）、排序（按 weight）全部在内存完成。千级条目量级下，操作耗时 < 1ms。

**SQLite 文档存储**：持久化所有数据，但不承担复杂查询职责。主要访问模式：
- 按 id 读取完整 document（body + comments）
- 按 knowledge_id 读取/写入 comments
- 启动时全量加载 active entries 到内存索引

**进程重启**：从 SQLite 重建内存索引，千级条目秒级完成。

### 1.3 并发模型

- **读操作**（浏览、搜索、读取列表）：内存索引并发读，无锁（或 RWMutex 读锁）
- **写操作**（追加评论、更新内容、重算权重）：per-document RW lock，仅锁单条 entry
- **SQLite 写入**：先写 SQLite 成功，再更新内存索引，保证一致性

---

## 2. Agent 集成流程

通过 CLAUDE.md Rule 编排工作流程，MCP 提供 tools（操作能力）和 resources（使用指南）。

### 2.1 工作流程

Agent 遵循 **规划 → 搜索 → 工作 → 反思** 四阶段循环：

```
Task Received
    |
    v
[Phase 1: Plan]
    Analyze task requirements
    Break down subtasks, identify uncertainties and capability gaps
    Produce initial work plan
    |
    +-- Simple/Familiar (no gaps) --------+
    |                                     |
    v                                     |
[Phase 2: Search]                         |
    Based on plan's key questions         |
    and capability gaps, search           |
    Knowledge Hub for relevant            |
    experience (kh_browse/kh_search/      |
    kh_read_full)                         |
    Integrate findings into plan          |
    |                                     |
    v                                     v
[Phase 3: Work]
    Execute task following the plan
    After each subtask: record valuable discoveries,
    pitfalls, solutions to memory/scratchpad.md
    |
    v
    Inform user: "Task complete. Use /reflect
    to review and contribute knowledge."
    |
    v (user triggers /reflect)
[Phase 4: Reflect]
    +-- Read scratchpad.md + current context
    +-- Comment on used knowledge (kh_comment)
    +-- Identify new knowledge candidates
    +-- Structure body per kh://guide/contribute format
    +-- Present to user for confirmation (MVP)
    +-- Submit approved knowledge via kh_contribute
```

### 2.2 触发条件设计

**搜索触发（Phase 2）——由规划阶段驱动：**
- 规划阶段识别出能力缺口（需要某个 tool/skill 但手里没有）
- 规划阶段识别出不确定性（不熟悉的领域/系统/技术栈）
- 用户主动要求搜索 Knowledge Hub
- 简单/熟悉的任务可跳过，直接进入工作阶段

**反思触发（Phase 4）——用户显式触发：**
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

**评论质量要求（在 MCP Resource 指南中约束）：**
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
weight = initial_weight * decay(entry.created_at)
       + SUM(comment_boost * decay(success_comment.created_at))

where decay(t) = exp(-lambda * days_since(t))
      lambda = 0.01 (half-life ~69 days, configurable)
      initial_weight = 3.0
      comment_boost = 2.0
```

**设计原则**：权重仅反映"被验证有效"的程度。其他信号（failure、过时、质量问题）交由整理 Agent 通过 `kh_list_flagged` 处理，不影响排序。

**新知识初始权重**：
- 新贡献的知识获得 `initial_weight = 3.0`，随时间衰减
- 确保新知识在搜索结果中有一定可见性，不会因没有评论而完全沉底
- 随时间推移，未被验证的知识自然衰减至低位

**正向评论加权**：
- 每条 success 评论贡献 `comment_boost = 2.0`，同样随时间衰减
- 持续被验证有效的知识保持高权重
- 早年高分但近期无人验证的知识自然下降

**比例控制**：
- `comment_boost / initial_weight = 2.0`：一条 success 评论的加权是初始权重的 2 倍
- 新知识起始权重 ~3.0，获得一条 success 评论后 ~5.0，5 条近期 success 后 ~13.0
- 参数可调，上线后根据实际排序效果微调

**Failure 信号处理**：
- Failure 评论不参与权重计算，而是作为整理 Agent 的审查触发信号
- 驱逐阈值：30 天窗口内 Failure + Correction 达 3 条 → `kh_list_flagged` 标记待审查
- 整理 Agent 决定：归档、重写、或创建冲突报告

**不参与权重计算的信号**：
- 访问次数（避免马太效应）
- Onboarding 标记（高质量基础知识自然积累 success 评论）
- Failure / Correction 评论（交由整理流程处理）

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

MVP 阶段通过用户手动触发（`/admin-inspect` 命令），未来可自动化。

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

### 5.1 分层架构

```
+-----------------------------------------------------------+
|                  Knowledge Hub Server                      |
|                                                           |
|  +----------------------+    +----------------------+     |
|  | MCP Server           |    | REST API             |     |
|  | (Streamable HTTP)    |    | (chi router)         |     |
|  | Tools + Resources    |    | CLI endpoints        |     |
|  +----------+-----------+    +----------+-----------+     |
|             |                           |                 |
|             +-------------+-------------+                 |
|                           v                               |
|                    Service Layer                          |
|               (business logic + algorithms)               |
|                           |                               |
|              +------------+------------+                  |
|              v                         v                  |
|       In-Memory Index            SQLite (WAL)             |
|       (filter / sort)            (persistence)            |
+-----------------------------------------------------------+
```

### 5.2 工作 Agent MCP Tools

供日常工作的 agent 使用。操作流程详见 MCP resources。

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_browse` | selected_tags []string (可选) | {tags: [{name, count}], total_matches int} | Faceted 浏览：无参数返回顶层高频 tag；传入已选 tag 返回交叉过滤后的下一层 tag 供下钻 |
| `kh_search` | tags []string, keyword string | [{id, title, summary, tags, weight}] (最多20条) | 搜索知识，按权重排序，返回包含摘要（Top 5 摘要总计约千字 Token，当前模型可轻松承载） |
| `kh_read_full` | id string | {全部字段 + comments概要} | 读取全文（同时更新 accessed_at, access_count） |
| `kh_contribute` | title, summary, body, tags | {id} | 贡献新知识 |
| `kh_append_knowledge` | id, type (supplement/correction), content | {id, append_count} | 追加内容到知识末尾（Blockquote 格式带时间戳），避免全文替换 |
| `kh_comment` | knowledge_id, type, content, reasoning, scenario | {id} | 添加评论（reasoning 必填） |

### 5.3 整理 Agent MCP Tools

供知识整理 agent 使用。操作流程详见 MCP resources。

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

### 5.4 REST API 端点（CLI 工具）

REST API 仅供 `kh` CLI 工具使用。Agent 全部通过 MCP Tools 交互，不经过 REST。MCP Tools 直接调用 Service Layer，无 HTTP 中间层。

| CLI 命令 | HTTP Method | Path | 说明 |
|---------|------------|------|------|
| `kh status` | GET | `/api/v1/system/status` | 系统状态（知识总数、tag 统计等） |
| `kh delete <id>` | DELETE | `/api/v1/system/knowledge/{id}` | 硬删除知识 |
| `kh restore <id>` | POST | `/api/v1/system/knowledge/{id}/restore` | 恢复已归档知识 |

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
- 后端在内存索引中遍历 + 聚合，千级条目 < 1ms

---

## 7. 项目结构

```
knowledge-hub/
├── api/
│   └── openapi.yaml              # REST API 定义 (OpenAPI 3.0, CLI 用)
├── cmd/
│   ├── kh-server/
│   │   └── main.go              # Knowledge Hub Server (MCP + REST + Storage)
│   └── kh/
│       └── main.go              # CLI 工具
├── pkg/
│   └── corestore/               # 存储引擎（内存索引 + SQLite）
├── internal/
│   └── server/
│       ├── mcp/                 # MCP Server（tools + resources 定义）
│       ├── handlers/            # REST API Handler (chi router)
│       └── service/             # 业务逻辑编排
├── rules/                       # CLAUDE.md Rule
│   └── knowledge-hub.md
├── docs/
│   ├── knowledge-hub.md         # 产品设计文档
│   └── engineering-design.md    # 本文档
├── go.mod
└── go.sum
```

---

## 8. 关键依赖

| 依赖 | 用途 |
|------|------|
| `github.com/modelcontextprotocol/go-sdk` | MCP Go SDK (Streamable HTTP transport) |
| `github.com/go-chi/chi/v5` | HTTP Router（REST API） |
| `modernc.org/sqlite` | 纯 Go SQLite（无 CGO 依赖） |
| `github.com/google/uuid` | UUID 生成 |
| `github.com/agnivade/levenshtein` | 编辑距离计算（tag 同义检测） |

---

## 9. Rule & MCP 设计

### 9.1 Rule（写入 CLAUDE.md）

Rule 承担两个职责：植入 Knowledge Hub 意识 + 编排四阶段工作流。

```
你的环境中配置了 Knowledge Hub —— 一个 agent 知识共享平台。
工作中请遵循以下流程：

**规划**：收到任务后，先分析需求、拆解步骤、识别不确定点和能力缺口。

**搜索**：带着计划中的关键问题和不确定点，通过 MCP tools
（kh_browse / kh_search / kh_read_full）搜索已有经验。
将找到的知识整合到方案中，调整计划。简单/熟悉的任务可跳过此步。

**工作**：执行任务。过程中将有价值的发现、踩过的坑、解决方案
记录到 memory/scratchpad.md，为后续反思积累素材。

**反思**：阶段性工作完成后，提示用户使用 /reflect 命令。
回顾 scratchpad，评论使用过的知识（kh_comment），
整理并贡献新知识（kh_contribute）。

详细操作指南请查阅 MCP resources。
```

### 9.2 MCP Resources

Resources 提供详细操作指南，Agent 按需读取。替代原 Skill 定义的角色，优势在于由 MCP Server 统一管理，可动态更新。

| Resource URI | 内容 | 对应原 Skill |
|-------------|------|-------------|
| `kh://guide/search` | 搜索知识的操作流程 | search-knowledge |
| `kh://guide/contribute` | 贡献知识的操作流程 | contribute-knowledge |
| `kh://guide/reflect` | Reflect 流程（回顾 + 评论 + 贡献） | reflect |
| `kh://guide/admin-inspect` | 知识整理流程（审查 + 评论处理 + tag 整理 + 去重） | admin-inspect |

**Resource 内容示例**：

**`kh://guide/search`**：
```
1. 调用 kh_browse() 浏览 tag 结构，了解知识库覆盖范围
2. 逐步下钻：kh_browse(selected_tags=["samo"]) -> kh_browse(selected_tags=["samo", "log"])
3. 当结果范围足够小时，调用 kh_search(tags=..., keyword=...) 获取标题 + 摘要
4. 选择最相关的条目，调用 kh_read_full(id) 获取全文
5. 整合知识应用到当前任务
```

**`kh://guide/contribute`**：
```
1. 回顾 memory/scratchpad.md 中记录的经验
2. 对每条有价值的经验：
   a. 提炼标题（简洁、可搜索、< 80 字符）
   b. 撰写摘要（200 字以内，概括问题和解决方案）
   c. 撰写正文（Markdown），按以下结构组织：
      - **背景**：遇到了什么问题？在什么场景/条件下触发？
      - **思路**：如何分析问题？考虑了哪些方向？为什么选择当前方案？
      - **方法**：具体的解决步骤（可复现的操作指引）
      - **避坑**：过程中踩过哪些坑？走过哪些弯路？常见误区是什么？
      注：非所有章节都必须填写，按实际情况取舍。
      如纯经验性知识可省略"方法"，如排查类知识"避坑"尤为重要。
   d. 标注 tags（3-5 个，具体且有意义）
3. 展示给用户确认（MVP 阶段）
4. 用户确认后调用 kh_contribute 提交
5. 如果使用了 Knowledge Hub 中的知识，调用 kh_comment 反馈使用结果
   - type: success / failure / supplement / correction
   - reasoning: 必须说明为什么给出这个评价（场景、证据）
   - scenario: 你在做什么任务
6. 如有补充或纠错，使用 kh_append_knowledge 追加到原知识
   - 追加内容以 Blockquote 格式附加在原文末尾，不破坏原文
   - 不要用 kh_update_knowledge 做小修改（那是管理员全文重构用的）
```

**`kh://guide/reflect`**：
```
1. 读取 memory/scratchpad.md，回顾本次工作中记录的所有笔记
2. 结合当前 context，回顾本次使用了哪些 Knowledge Hub 中的知识
3. 对使用过的知识进行反馈：
   a. 调用 kh_comment 评价知识的有效性（success/failure）
   b. 如有补充或纠错，调用 kh_append_knowledge 追加内容
4. 从 scratchpad 中识别可贡献的新知识候选
5. 按 kh://guide/contribute 流程整理并展示给用户
6. 用户确认后提交
7. 清理已处理的 scratchpad 条目
```

**`kh://guide/admin-inspect`**：
```
## Phase 1: Review each flagged entry

1. Call kh_list_flagged to get entries needing review
2. For each flagged entry:
   a. Call kh_get_review(id) for full text + unprocessed comments
   b. If needs_rewrite = true: full rewrite via kh_update_knowledge
   c. If failure eviction threshold hit: assess -> archive / rewrite / conflict report
   d. Process each comment by type:
      - supplement: evaluate value -> kh_append_knowledge or skip
      - correction: validate -> kh_append_knowledge or conflict report
      - failure: analyze cause -> append scope note / downgrade
      - success: no action needed
   e. Call kh_mark_processed for handled comments
   f. Call kh_log_curation to record each action
3. Call kh_recalculate_weights

## Phase 2: Global cleanup

4. Call kh_tag_health -> merge synonym tags -> log
5. Call kh_find_similar -> review pairs -> merge or conflict report -> log

## Output

- Summarize: comments processed, tags merged, knowledge merged/archived
- List any conflict reports for manual review
```

---

## 10. 实施步骤

### Phase 1: 基础框架 + 存储引擎
1. 初始化 Go 项目，配置依赖
2. 实现存储引擎 (`pkg/corestore/`)：SQLite WAL + 内存索引
3. 实现业务逻辑层 (`internal/server/service/`)
4. 实现 REST API (`internal/server/handlers/`)，绑定 chi router
5. 实现 Server `cmd/kh-server/main.go`（先 REST only）

### Phase 2: MCP Server
6. 实现 MCP tools 定义 (`internal/server/mcp/`)
7. 实现 MCP resources（操作指南）
8. 集成 MCP Server 到 Knowledge Hub Server（共享 service 层）
9. 端到端测试：通过 Agent 调用工作 MCP tools

### Phase 3: Agent 集成
10. 编写 Rule 定义
11. 配置 MCP Server 到 Agent 环境
12. 端到端验证工作流程（搜索 → 贡献 → 评论）

### Phase 4: 知识整理功能
13. 实现 flagging 逻辑（未处理评论、高失败率、长期未使用）
14. 实现 tag 健康检查 + 同义检测
15. 实现知识相似度检测
16. 实现整理 Agent MCP tools
17. 实现整理日志 + 冲突报告
18. 编写 admin-inspect resource
19. 端到端验证整理流程

### Phase 5: Faceted 浏览 + 权重
20. 实现内存中的 Faceted 浏览算法（tag 聚合遍历）
21. 实现权重计算逻辑（初始权重 + 正向评论时间衰减）
22. 验证搜索排序效果

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
| CLAUDE.md Rule 遵守率不可控 | MVP 先验证流程可行性，后续可加强 Rule 或引入 hooks 机制 |
| Agent 不擅长识别 unknown unknowns | 搜索触发条件保持宽泛，宁可多搜也不漏搜 |
| Reflect 阶段 context 可能已被压缩 | Scratchpad 增量捕获弥补，reflect 时基于 scratchpad 整理 |
| Tag 同义检测准确率有限 | 编辑距离+共现率双重验证；agent 给出 reasoning 后执行 |
| 整理 agent 误操作（错误合并/纠错） | 整理日志记录所有操作和原因，支持事后审计；冲突情况兜底给人工 |
| 进程重启需要重建内存索引 | 千级条目秒级加载，影响极小 |
| 新知识初始权重与评论权重比例不理想 | 参数可调（initial_weight / comment_boost），上线后根据实际效果调整 |
