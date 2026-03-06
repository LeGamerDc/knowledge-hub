# Knowledge Hub 工程设计

## Context

基于 `docs/knowledge-hub.md` 的产品设计，我们需要将其转化为可落地的工程方案。本方案是 MVP（本地单用户），核心验证 tag 系统 + 评论系统 + agent 集成流程的可行性。通过 CLAUDE.md Rule 编排 agent 工作流程，MCP 提供 tools + resources。

### 设计变更记录

| 变更 | 原方案 | 现方案 | 理由 |
|------|-------|-------|------|
| 进程间通信协议 | gRPC + Protobuf | OpenAPI 3.0 + HTTP/JSON | MVP 阶段优先调试友好性：HTTP 可直接 curl 调试；oapi-codegen 提供等价的强类型代码生成（Server Handler + Client）；省去 protoc + gRPC 插件工具链 |
| 进程模型 | MCP Shim（stdio）+ API Server（HTTP）双进程 | 单进程 Knowledge Hub Server（HTTP MCP + REST API） | MCP 协议已支持 Streamable HTTP，Agent 可直连，Shim 的协议转换层不再必要 |
| 存储架构 | SQLite 承担所有查询（复杂索引 + SQL） | 内存索引（筛选/排序）+ SQLite 文档存储（持久化） | MVP 数据量级（千级条目）无需数据库查询引擎，内存操作更简单高效 |
| 排序机制 | 多因素公式（success/failure/access/onboarding） | success_comment_count（成功评论计数） | 简单直接，failure 信号交由整理 Agent 处理而非影响排序 |
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
|  - success_comment  |       |    (per knowledge)  |
|    _count           |       |                     |
|  - status           |       |  tags table         |
|  - created_at       |       |                     |
+---------------------+       |                     |
                              +---------------------+
```

**内存索引**：所有 active knowledge entry 的 `id / title / summary / tags / success_comment_count / status / created_at` 载入内存。浏览（tag 聚合 + 层级导航）、排序（按 success_comment_count）全部在内存完成。千级条目量级下，操作耗时 < 1ms。

**SQLite 文档存储**：持久化所有数据，但不承担复杂查询职责。主要访问模式：
- 按 id 读取完整 document（body + comments）
- 按 knowledge_id 读取/写入 comments
- 启动时全量加载 active entries 到内存索引

**进程重启**：从 SQLite 重建内存索引，千级条目秒级完成。

### 1.3 并发模型

- **读操作**（浏览、读取列表）：内存索引并发读，无锁（或 RWMutex 读锁）
- **写操作**（追加评论、更新内容、重算权重）：per-document RW lock，仅锁单条 entry
- **SQLite 写入**：先写 SQLite 成功，再更新内存索引，保证一致性

---

## 2. Agent 集成流程

通过 CLAUDE.md Rule 编排工作流程，MCP 提供 tools（操作能力）和 resources（使用指南）。

### 2.1 工作流程

Agent 遵循 **规划 → 查阅 → 工作 → 反思** 四阶段循环：

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
[Phase 2: Browse]                         |
    Based on plan's key questions         |
    and capability gaps, browse           |
    Knowledge Hub for relevant            |
    experience (kh_browse / kh_read_full) |
    Integrate findings into plan          |
    |                                     |
    v                                     v
[Phase 3: Work]
    Execute task following the plan
    After each subtask: record valuable discoveries,
    pitfalls, solutions to memory/scratchpad.md
    |
    v
[Phase 4: Reflect]
    Agent autonomously reflects based on Rule guidance.
    +-- Read scratchpad.md + conversation context + git diff + any available info
    +-- Comment on used knowledge (kh_comment)
    +-- Identify new knowledge candidates
    +-- Structure body per kh://guide/contribute format
    +-- Present to user for confirmation (MVP)
    +-- Submit approved knowledge via kh_contribute
    If agent does not auto-reflect, user can trigger via /reflect as fallback.
```

### 2.2 触发条件设计

**查阅触发（Phase 2）——由规划阶段驱动：**
- 规划阶段识别出能力缺口（需要某个 tool/skill 但手里没有）
- 规划阶段识别出不确定性（不熟悉的领域/系统/技术栈）
- 用户主动要求浏览 Knowledge Hub
- 简单/熟悉的任务可跳过，直接进入工作阶段

**反思触发（Phase 4）——Agent 自主触发为主：**
- Agent 根据 Rule 指引，在阶段性工作完成后自主进入反思阶段
- Agent 应利用一切可获取的信息源：scratchpad、conversation context、git diff 等
- 用户输入 `/reflect` 命令作为兜底（当 Agent 未自动反思时）

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

Reflect 阶段 Agent 应利用一切可获取的信息源进行深度整理和知识贡献：scratchpad 笔记、conversation context、git diff、工作产出等。

---

## 3. 数据模型

### 3.1 Knowledge Entry

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| title | string | 标题，用于列表展示 |
| summary | string | 摘要，浏览时随标题一起展示 |
| body | string | 正文（Markdown） |
| tags | []string | 标签列表 |
| success_comment_count | int | 成功评论数，影响排序（越多越靠前） |
| author | string | 贡献者标识 |
| status | enum | active / archived |
| created_at | timestamp | 创建时间 |
| updated_at | timestamp | 更新时间 |
| accessed_at | timestamp | 最后被读取的时间（用于检测长期未使用） |
| access_count | int | 被读取次数 |

### 3.2 Comment

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| knowledge_id | string | 关联知识条目 |
| type | enum | success / failure / supplement / correction |
| content | string | 评论内容 |
| reasoning | string | failure/supplement/correction 必填，success **禁填**（见下方说明） |
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
- `success` 类型：`reasoning` 禁止填写。"有用"本身就是信息，scenario 字段已足够描述使用场景，强制 reasoning 只会产生空话噪声
- `failure` / `correction` / `supplement` 类型：`reasoning` 必填，必须说明 WHY（证据、上下文）
- `correction` 类型必须在 content 中指出原文哪里错、正确内容是什么
- `supplement` 类型必须在 content 中提供具体的补充信息

### 3.3 Tag

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string (UUID) | 主键 |
| name | string | 标准名称 |
| aliases | []string | 别名列表（归一化用） |
| frequency | int | 使用频次（知识条目中出现的次数） |

### 3.4 CurationLog（整理日志）— MVP 推迟

> MVP 阶段整理操作通过 Server 日志（stdout）记录，不单独建表。待实际运营需要结构化审计时再引入。

### 3.5 ConflictReport（冲突文档）— MVP 推迟

> MVP 阶段数据量小，冲突极少。无法判定的情况由管理员 Agent 通过 CLI/人工干预处理，不单独建表。

### 3.6 排序机制

**排序字段**：`success_comment_count`（成功评论数）。

每当知识收到一条 `success` 类型的评论，`success_comment_count` +1。浏览结果中，同一层级的文档按此字段降序排列。

**设计原则**：
- 简单直接：被更多 Agent 验证有效的知识排在前面
- 新知识起始为 0，不会被人为置顶，通过积累真实评论获得排名
- Failure 评论不影响排序，而是作为整理 Agent 的审查触发信号
- 访问次数不参与排序（避免马太效应）

**Failure 信号处理**：
- Failure 评论不参与排序，作为整理 Agent 的审查触发信号
- 驱逐阈值：30 天窗口内 Failure + Correction 达 3 条 → `kh_list_flagged` 标记待审查
- 整理 Agent 决定：归档或编辑修正

### 3.7 知识更新策略：只追加

**核心问题**：要求 LLM 为一个小纠错在 Tool Call 中输出完整的几千字 Markdown 正文，极易触发 Output Token 截断，且经常破坏原有的代码块缩进或排版格式。

**解决方案**：工作 Agent 只通过 `kh_append_knowledge` 追加增量内容，不做全文替换。管理员 Agent 在巡检时如有需要可通过 `kh_update_knowledge` 全量编辑。

**追加格式示例**：

```markdown
> **[Supplement] 2026-02-28T14:30:00**
> 补充：在 K8s 环境下，还需要配置 graceful shutdown 的 preStop hook，
> 给 load balancer 足够时间摘除 pod。
```

日常的补充和纠错只需输出增量内容（几十到几百字），彻底规避了 LLM 长文本输出的不稳定性。

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
    +-- Entries hitting failure eviction threshold (see 3.6)
    +-- Each entry includes: comment aggregation + flag reasons
    |
    v
[Step 1.2] Agent reviews each entry
    For each flagged knowledge:
    |
    +-- Call kh_get_review(id) to get full text + all unprocessed comments
    |
    +-- If hitting failure eviction threshold:
    |   +-- Assess whether knowledge is fundamentally flawed or just outdated
    |   +-- Flawed -> archive directly (kh_archive)
    |   +-- Outdated but salvageable -> edit with updated info (kh_update_knowledge)
    |
    +-- Process supplement comments:
    |   +-- Evaluate value and accuracy of supplementary content
    |   +-- Valuable -> call kh_append_knowledge
    |   +-- Not valuable -> mark as processed only
    |
    +-- Process correction comments:
    |   +-- Compare original text with correction
    |   +-- Correction valid -> call kh_append_knowledge with correction note
    |   +-- Correction invalid -> mark as processed only
    |
    +-- Process failure comments:
    |   +-- Analyze failure cause
    |   +-- Outdated -> append scope/version note or archive
    |   +-- Scenario mismatch -> append scope clarification
    |   +-- Unclear description -> append clarification note
    |
    +-- Call kh_mark_processed(comment_ids)
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
    |   +-- Exact duplicate -> archive the less complete one
    |   +-- Partial overlap -> append complementary content to the better one, archive the other
    |   +-- Appears similar but actually different -> keep both, adjust tags
    |   +-- Cannot determine -> skip, leave for human review via CLI
```

注：不再需要 rebuild_directory 步骤——Faceted 浏览基于实时查询，tag 合并和知识合并后自动反映在浏览结果中。

### 4.3 未来自动化路径

当积累足够的运营数据后：
1. 高置信度操作（编辑距离 ≤ 1 的 tag 合并）可自动执行
2. 中置信度操作自动生成待审批列表
3. 定期自动触发巡检（cron）
4. 引入结构化整理日志（CurationLog）和冲突管理（ConflictReport）

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
| `kh_browse` | selected_tags []string (可选) | {tags: [{name, count}], entries: [{id, title, summary, tags}], total_matches int} | Tag 层级浏览：无参数返回顶层高频 tag；传入已选 tag 返回下一层 tag + 当前层级文档；匹配文档 < 20 时直接返回文档列表 |
| `kh_read_full` | id string | {全部字段 + comments概要} | 读取全文（同时更新 accessed_at, access_count） |
| `kh_contribute` | title, summary, body, tags | {id} | 贡献新知识 |
| `kh_append_knowledge` | id, type (supplement/correction), content | {id} | 追加内容到知识末尾（Blockquote 格式带时间戳），避免全文替换 |
| `kh_comment` | knowledge_id, type, content, reasoning?, scenario | {id} | 添加评论（reasoning：success 类型禁填，其他类型必填） |

### 5.3 整理 Agent MCP Tools

供知识整理 agent 使用。操作流程详见 MCP resources。

**发现问题（API Server 算法驱动）：**

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_list_flagged` | 无 | [{id, title, flag_reasons[], comment_stats}] | 列出需要审查的知识（有未处理评论、高失败率、长期未使用、failure eviction threshold） |
| `kh_tag_health` | 无 | {similar_pairs[], low_freq[], high_freq[]} | Tag 健康度报告 |
| `kh_find_similar` | 无 | [{id_a, title_a, id_b, title_b, overlap_tags[], overlap_ratio}] | 疑似重复知识对 |

**审查详情：**

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_get_review` | id string | {knowledge全文 + 所有未处理comments} | 获取知识审查详情 |

**执行操作：**

| Tool | 参数 | 返回 | 说明 |
|------|------|------|------|
| `kh_update_knowledge` | id, title?, summary?, body?, tags? | {updated_at} | 全量更新知识内容（仅用于管理员 Agent 编辑） |
| `kh_archive` | id string | {} | 归档（软删除）知识 |
| `kh_mark_processed` | comment_ids []string | {} | 标记评论为已处理 |
| `kh_merge_tags` | target string, sources []string | {affected_knowledge_count} | 合并 tag，所有 sources 的知识重新打标为 target |

> **MVP 推迟的 Tools**：`kh_merge_knowledge`（用 archive + contribute 手动处理）、`kh_create_conflict`（人工 CLI 兜底）、`kh_log_curation`（stdout 日志替代）、`kh_recalculate_weights`（success_comment_count 自动维护，无需批量重算）

### 5.4 REST API 端点（CLI 工具）

REST API 仅供 `kh` CLI 工具使用。Agent 全部通过 MCP Tools 交互，不经过 REST。MCP Tools 直接调用 Service Layer，无 HTTP 中间层。

| CLI 命令 | HTTP Method | Path | 说明 |
|---------|------------|------|------|
| `kh status` | GET | `/api/v1/system/status` | 系统状态（知识总数、tag 统计等） |
| `kh delete <id>` | DELETE | `/api/v1/system/knowledge/{id}` | 硬删除知识 |
| `kh restore <id>` | POST | `/api/v1/system/knowledge/{id}/restore` | 恢复已归档知识 |

### 5.5 渐进式浏览 Token 控制

| 层级 | 返回内容 | 预估 Token |
|------|---------|-----------|
| Tag 层级浏览（匹配 >= 20） | tag 列表 + 当前层级文档（叶子 + 未分类） | ~300-800 |
| 文档列表（匹配 < 20） | 标题 + 摘要 + tags（最多 20 条） | ~500-1500 |
| 全文 | 完整 Markdown + 评论概要 | ~500-2000 |

---

## 6. 动态 Tag 层级浏览算法

**设计隐喻**：文件系统。Tag = 目录，文档 = 文件。Agent 像翻阅文件夹一样逐层导航，不依赖关键词搜索。

**设计变更**：放弃预生成静态目录树（组合爆炸），改为动态按需计算。放弃关键词搜索（Agent 不知道搜什么，但能从当前层级的 tag 列表中识别自己关注的方向）。

```
kh_browse(selected_tags=[])

Input: selected_tags (already chosen tags for filtering, can be empty)
Output: {tags: [{name, count}], entries: [{id, title, summary, tags}], total_matches: int}

Algorithm:
1. Filter all active knowledge entries that contain ALL selected_tags
   (if selected_tags is empty, all active entries match)
2. total_matches = count of filtered entries
3. If total_matches < 20:
   -> entries = all filtered entries (with title + summary)
   -> tags = [] (no further drilling needed)
   -> return
4. For each filtered entry, compute remaining_tags = entry.tags - selected_tags
5. Leaf entries: remaining_tags is empty -> add to entries (current level "files")
6. Non-leaf entries: count frequency of each remaining_tag, pick top N (default 15)
7. Uncategorized entries: remaining_tags are ALL outside top N
   -> also add to entries (visible at current level, not hidden)
8. Return tags (top N) + entries (leaf + uncategorized) + total_matches
```

**示例交互**：
```
Agent: kh_browse()
  -> tags: [{go, 45}, {debugging, 38}, {k8s, 25}, {hiring, 10}, ...]
     entries: [{doc_with_no_tags_1}]    // docs without any tag
     total: 120

Agent: kh_browse(selected_tags=["go"])
  -> tags: [{concurrency, 12}, {http, 10}, {testing, 8}, ...]
     entries: [{doc_tagged_only_go_1}]  // docs with only "go" tag, no remaining tags
     total: 45

Agent: kh_browse(selected_tags=["go", "concurrency"])
  -> total: 12, less than 20
     tags: []
     entries: [all 12 matching docs with title + summary]
```

**优势**：
- 无需预计算或缓存目录树，零存储开销
- 支持任意 tag 组合的自由探索，不受预定义层级限制
- 文档不会因 tag 不在高频列表中而"隐藏"——未分类文档直接展示在当前层级
- 后端遍历 + 聚合，千级条目 < 1ms

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

> **关于 Rule 长度**：产品设计准则是"一句话告知 Agent 平台的存在"，这是长期愿景。MVP 阶段 Agent 需要更明确的流程指引来确保遵循工作流，因此 Rule 包含结构化的四阶段说明。随着 Agent 能力提升，Rule 可逐步精简。

```
你的环境中配置了 Knowledge Hub —— 一个 agent 知识共享平台。
工作中请遵循以下流程：

**规划**：收到任务后，先分析需求、拆解步骤、识别不确定点和能力缺口。

**查阅**：带着计划中的关键问题和不确定点，通过 MCP tools
（kh_browse / kh_read_full）浏览已有经验。
逐层选择 tag 缩小范围，找到相关知识后整合到方案中。
简单/熟悉的任务可跳过此步。

**工作**：执行任务。过程中将有价值的发现、踩过的坑、解决方案
记录到 memory/scratchpad.md，为后续反思积累素材。

**反思**：阶段性工作完成后，回顾 scratchpad 及一切可获取的信息
（conversation context、git diff、工作产出等），
评论使用过的知识（kh_comment），整理并贡献新知识（kh_contribute）。
如果未自动进入反思，用户可通过 /reflect 命令触发。

详细操作指南请查阅 MCP resources。
```

### 9.2 MCP Resources

Resources 提供详细操作指南，Agent 按需读取。替代原 Skill 定义的角色，优势在于由 MCP Server 统一管理，可动态更新。

| Resource URI | 内容 | 对应原 Skill |
|-------------|------|-------------|
| `kh://guide/browse` | 浏览知识的操作流程 | search-knowledge |
| `kh://guide/contribute` | 贡献知识的操作流程 | contribute-knowledge |
| `kh://guide/reflect` | Reflect 流程（回顾 + 评论 + 贡献） | reflect |
| `kh://guide/admin-inspect` | 知识整理流程（审查 + 评论处理 + tag 整理 + 去重） | admin-inspect |

**Resource 内容示例**：

**`kh://guide/browse`**：
```
1. 调用 kh_browse() 浏览顶层 tag 结构，了解知识库覆盖范围
2. 逐层选择感兴趣的 tag 下钻：kh_browse(selected_tags=["go"]) -> kh_browse(selected_tags=["go", "concurrency"])
3. 当匹配文档 < 20 时，kh_browse 直接返回文档列表（标题 + 摘要）
4. 当前层级也可能直接展示未分类文档（没有更多 tag 可下钻的文档）
5. 选择最相关的条目，调用 kh_read_full(id) 获取全文
6. 整合知识应用到当前任务
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
   - reasoning: failure/supplement/correction 必填（场景、证据）；success 禁填
   - scenario: 你在做什么任务
6. 如有补充或纠错，使用 kh_append_knowledge 追加到原知识
   - 追加内容以 Blockquote 格式附加在原文末尾，不破坏原文
   - 不要用 kh_update_knowledge 做小修改（那是管理员全文重构用的）
```

**`kh://guide/reflect`**：
```
1. 利用一切可获取的信息源回顾本次工作：
   - memory/scratchpad.md 中的笔记
   - conversation context（对话历史）
   - git diff（代码变更）
   - 工作产出和遇到的问题
2. 回顾本次使用了哪些 Knowledge Hub 中的知识
3. 对使用过的知识进行反馈：
   a. 调用 kh_comment 评价知识的有效性（success/failure）
   b. 如有补充或纠错，调用 kh_append_knowledge 追加内容
4. 识别可贡献的新知识候选
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
   b. If failure eviction threshold hit: assess -> archive (kh_archive) or edit (kh_update_knowledge)
   c. Process each comment by type:
      - supplement: evaluate value -> kh_append_knowledge or skip
      - correction: validate -> kh_append_knowledge or skip
      - failure: analyze cause -> append scope note / archive
      - success: no action needed
   d. Call kh_mark_processed for handled comments

## Phase 2: Global cleanup

3. Call kh_tag_health -> merge synonym tags (kh_merge_tags)
4. Call kh_find_similar -> review pairs -> archive duplicates or adjust tags

## Output

- Summarize: comments processed, tags merged, knowledge archived
- Note any items that could not be resolved (leave for human review via CLI)
```

---

## 10. 实施步骤

### Phase 1: 基础框架 + 存储引擎
1. 初始化 Go 项目，配置依赖
2. 实现存储引擎 (`pkg/corestore/`)：SQLite WAL + Tag 层级浏览算法
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
12. 端到端验证工作流程（浏览 → 贡献 → 评论）

### Phase 4: 知识整理功能
13. 实现 flagging 逻辑（未处理评论、高失败率、长期未使用）
14. 实现 tag 健康检查 + 同义检测
15. 实现知识相似度检测
16. 实现整理 Agent MCP tools
17. 编写 admin-inspect resource
18. 端到端验证整理流程

---

## 11. 验证方案

1. **浏览场景**: 手动录入几条知识 → 通过 kh_browse 逐层 tag 下钻 → 验证混合返回（tag + 文档）正确 → kh_read_full 读取全文
2. **贡献场景**: 执行一个任务 → scratchpad 记录 → reflect 阶段通过 kh_contribute 贡献知识
3. **评论场景**: 使用一条知识 → 通过 kh_comment 反馈结果 → 验证 success_comment_count 变化
4. **整理场景（Phase 1）**: 添加 supplement/correction 评论 → 触发 admin-inspect → 验证知识更新
5. **整理场景（Phase 2）**: 贡献多条含相似 tag 的知识 → 触发 admin-inspect → 验证同义检测和合并
6. **完整流程**: 从任务开始到结束，走通 规划 → 查阅 → 执行 → reflect 全流程

---

## 12. 已知风险与妥协

| 风险 | 妥协方案 |
|------|---------|
| CLAUDE.md Rule 遵守率不可控 | MVP 先验证流程可行性，后续可加强 Rule 或引入 hooks 机制 |
| Agent 不擅长识别 unknown unknowns | Tag 层级浏览降低门槛，Agent 看到 tag 列表即可发现相关方向 |
| Reflect 阶段 context 可能已被压缩 | Scratchpad 增量捕获弥补，reflect 时利用一切可获取信息源 |
| Tag 同义检测准确率有限 | 编辑距离+共现率双重验证；agent 给出 reasoning 后执行 |
| 整理 agent 误操作（错误归档/纠错） | Server 日志记录操作轨迹，无法判断的情况兜底给人工 CLI |
| 进程重启需要重建内存索引 | 千级条目秒级加载，影响极小 |
