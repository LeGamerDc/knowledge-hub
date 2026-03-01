# Stage 3: 存储引擎 — 检索与算法层

> 目标：实现 `pkg/corestore` 的所有算法驱动功能：搜索、Faceted 浏览、权重计算、Flagging、相似度检测、Tag 健康检查。

## 前置条件

- Stage 2 完成（基础 CRUD 可用，表结构就绪）

## 任务清单

### 3.1 Search 实现

- [ ] `Search(ctx, query) → ([]*KnowledgeEntry, error)`
- [ ] 支持 tag 过滤（交集：必须包含所有指定 tag）
- [ ] 支持 keyword 搜索（标题 + 摘要的 LIKE 匹配）
- [ ] 按权重降序排序
- [ ] 限制返回最多 20 条
- [ ] 仅返回 status=ACTIVE 的条目
- [ ] 返回字段：id, title, summary, tags, weight

### 3.2 BrowseFacets 实现

- [ ] `BrowseFacets(ctx, selectedTags) → (*FacetResult, error)`
- [ ] 空 selectedTags：统计所有 ACTIVE 知识的 tag 频率，返回 Top N（默认 15）
- [ ] 非空 selectedTags：
  1. 交集过滤：找到同时包含所有 selectedTags 的 ACTIVE 知识
  2. 频次聚合：对这些知识的剩余 tag（排除已选 tag）做 COUNT
  3. 返回 Top N 剩余 tag + total_matches
- [ ] SQL 优化：利用 knowledge_tags 索引做高效聚合

### 3.3 RecalculateWeights 实现

- [ ] `RecalculateWeights(ctx) → (updatedCount, error)`
- [ ] 权重公式：
  ```
  weight = base_weight(1.0)
         + SUM(exp(-0.01 * days_since(success_comment)) * 1.0)  // 未处理的 success 衰减加分
         - unprocessed_failure_count * 2.0                       // 未处理的 failure 扣分
         + access_count * 0.1
  ```
- [ ] 时间衰减参数可配置（lambda 默认 0.01，half-life ~69 天）
- [ ] 已处理的评论不参与计算
- [ ] 批量更新所有 ACTIVE 条目

### 3.4 ListFlagged 实现

- [ ] `ListFlagged(ctx) → ([]*FlaggedEntry, error)`
- [ ] 多条件标记（任一条件满足即 flag）：
  - 有未处理评论（processed=false）
  - Failure 评论占比 > 50%（在该知识的所有评论中）
  - 30+ 天未被访问（accessed_at）
  - needs_rewrite = true（append_count 超阈值）
  - Failure eviction：30 天滑动窗口内 failure + correction >= 3 条
- [ ] 返回 flag_reasons 列表 + comment_stats

### 3.5 FindSimilar 实现

- [ ] `FindSimilar(ctx) → ([]*SimilarPair, error)`
- [ ] 检测 Tag 重叠率 > 80% 的知识对
- [ ] 仅检测 ACTIVE 条目
- [ ] 返回：两条知识的 id/title + 重叠 tags + 重叠率

### 3.6 GetTagHealth 实现

- [ ] `GetTagHealth(ctx) → (*TagHealthReport, error)`
- [ ] 疑似同义 Tag 对检测（满足任一）：
  - 编辑距离 <= 2（使用 agnivade/levenshtein）
  - 一个是另一个的子串
  - 共现率 > 80%（两个 tag 经常出现在同一篇知识中）
  - 已有别名映射匹配
- [ ] 低频 Tag：frequency < 3
- [ ] 异常高频 Tag：占比 > 30%

### 3.7 集成测试

- [ ] 搜索测试：插入多条知识 → 按 tag/keyword 搜索 → 验证结果和排序
- [ ] Faceted 浏览测试：多层下钻场景 → 验证 tag 聚合和 total_matches
- [ ] 权重计算测试：插入带评论的知识 → 重算 → 验证公式正确性
- [ ] Flagging 测试：构造各种 flag 条件 → 验证 ListFlagged 返回
- [ ] 相似度测试：插入高重叠知识 → 验证 FindSimilar 检测
- [ ] Tag 健康测试：插入相似 tag → 验证同义检测

## 交付物

- `pkg/corestore/` 新增文件：search.go, browse.go, weight.go, flagging.go, similar.go, taghealth.go
- 全部集成测试通过
- Store 接口 100% 实现

## 参考文档

- `docs/engineering-design.md` §6 — Faceted 浏览算法
- `docs/engineering-design.md` §3.6 — 权重计算公式
- `docs/specs/storage-engine.md` §3 — 分面检索算法, §5 — 权重计算
