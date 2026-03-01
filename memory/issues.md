# 问题与解决方案

<!-- 格式：[日期] 问题标题 -->
<!-- Problem: 遇到了什么 -->
<!-- Root Cause: 根因 -->
<!-- Solution: 怎么解决的 -->

## [2026-03-01] BrowseFacets 小数据集返回空 tags

**Problem**: `kh_browse` 在知识库条目 ≤ 10 时返回空 tags 数组，即使确实存在标签数据。

**Root Cause**: `BrowseFacets` 实现中，当 `len(entryIDs) <= 10` 时走了一个短路分支，直接返回 `Entries`（原始条目列表）而不计算 `NextTags`。但 Service 层只读 `NextTags`，导致 `Tags` 为空。这与 API 设计矛盾——设计中"何时切换到 kh_search"应由 Agent 决定，Server 始终应返回 tags。

**Solution**: 移除 `<= 10` 的短路分支，`BrowseFacets` 始终计算并返回 `NextTags`。更新相关测试以验证 `NextTags` 而非 `Entries`。

## [2026-03-01] corestore.Search 按 Tag 过滤时返回空结果

**Problem**: `AgentSearch` 传入 tags 参数时，搜索结果为空，即使数据库中存在匹配条目。

**Root Cause**: `buildTagJoin` 在 SQL JOIN 子句中使用 `?` 占位符（JOIN 出现在 WHERE 之前），但在 `args` 构建时，status/keyword 参数被先追加，tag 参数被后追加（通过 `buildTagJoin` 副作用修改 `&args`）。这导致 SQLite 驱动将 status/keyword 值绑定到 JOIN 的占位符，tag 值绑定到 WHERE 的占位符，参数顺序完全错误。

**Solution**: 重构 `Search` 方法，将 JOIN 参数（`tagArgs`）和 WHERE 参数（`whereArgs`）分开收集，最终组合为 `tagArgs + whereArgs + [limit, offset]`，保证与 SQL 中占位符出现顺序一致。

## [2026-03-01] corestore.Search status=0 时默认过滤 ACTIVE

**Problem**: `SearchQuery.Status` 注释说明 `0=不过滤`，但实现中 `else` 分支将 status=0 也处理为仅返回 ACTIVE 条目，导致 `SystemListKnowledge`（全量列出）无法正常工作。

**Root Cause**: `Search` 方法 `if q.Status != 0 { ... } else { ... }` 的 else 分支错误地添加了 `status = KnowledgeStatusActive` 条件。

**Solution**: 去掉 else 分支，status=0 时不添加任何 status 过滤条件。

## [2026-03-01] GetByID 在 AgentAppendKnowledge 中引入 access_count 副作用

**Problem**: `Append` store 方法只返回 error，Service 层需要新的 `append_count` 来构建响应，因此调用了 `GetByID`。但 `GetByID` 会额外更新 `access_count` 和 `accessed_at`，这对 append 操作是不合语义的。

**Root Cause**: Store 接口没有提供只读取 `append_count` 的轻量方法。

**Solution**: MVP 阶段接受此副作用（一次额外的 access_count+1），通过注释说明。后续可在 Store 接口添加 `GetAppendCount` 轻量方法解决。同样地，`SystemGetKnowledge` 理论上不应更新 access stats，但当前也使用 `GetByID`，为同一类 MVP 妥协。
