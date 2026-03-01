# Skill: Knowledge Hub Admin Inspection

知识库全库巡检与整理。由管理员用户手动触发。

你的职责是**降噪和归一化**，不处理具体业务。
按以下两个 Phase 顺序执行：

---

## Phase 1：逐条知识审查

### 1.1 获取待审查列表

调用 `kh_list_flagged` 获取被标记的知识条目。Server 会返回以下类别：
- 有未处理评论（`has_unprocessed_comments`）
- 高失败率（`high_failure_rate`，failure+correction 评论 > 50%）
- 长期未访问（`stale_access`，30+ 天无人读取）
- 需要全文重构（`needs_rewrite`，追加次数超过阈值）
- 触发驱逐阈值（`failure_eviction`，30 天内 ≥ 3 条 failure/correction）

### 1.2 逐条处理

对每条 flagged 知识：

**a. 获取详情**：调用 `kh_get_review(id)` 获取全文 + 所有未处理评论。

**b. 若 `needs_rewrite = true`**：
- 读取全文（含所有追加的 blockquote）
- 执行全文融合重写：将所有追加内容融合进正文，保持逻辑连贯
- 调用 `kh_update_knowledge`（全量替换，重置 `append_count`）

**c. 若触发 `failure_eviction`**：
- 评估：根本性错误 → 调用 `kh_archive` 直接归档
- 评估：仅过时但可修复 → 重写更新后调用 `kh_update_knowledge`
- 无法判断 → 调用 `kh_create_conflict` 创建冲突报告，上报人工

**d. 处理每条未处理评论**：

| 评论类型 | 处理方式 |
|---------|---------|
| `supplement` | 评估价值和准确性 → 有价值则 `kh_append_knowledge` 追加；无价值则仅标记 |
| `correction` | 对比原文和纠错内容 → 成立则 `kh_append_knowledge` 追加纠错说明；存疑则 `kh_create_conflict` |
| `failure` | 分析原因：过时 → 追加版本/范围说明；场景不匹配 → 追加适用范围澄清；描述不清 → 追加说明 |
| `success` | 无需特殊操作，记录即可 |

**e. 标记已处理**：调用 `kh_mark_processed` 传入所有已处理的评论 ID。

**f. 记录整理日志**：调用 `kh_log_curation` 记录每个实质性操作：
- `action`：操作类型（`apply_correction` / `merge_supplement` / `archive` / `downgrade` / `resolve_conflict`）
- `target_id`：被操作的知识 ID
- `source_ids`：触发此操作的评论 IDs
- `description`：具体做了什么、为什么（必须有 reasoning）
- `diff`：若修改了正文，记录 diff（旧 → 新的关键内容）

### 1.3 重算权重

调用 `kh_recalculate_weights` 刷新所有知识权重。

---

## Phase 2：全局整理

### 2.1 Tag 整理

调用 `kh_tag_health` 获取 Tag 健康报告，包含：
- 疑似同义 tag 对（编辑距离 ≤ 2、子串关系、共现率 > 80%）
- 低频 tag（< 3 次使用）
- 异常高频 tag（> 30% 占比，粒度可能过粗）

对每组疑似同义 tag：
1. 判断是否真的同义
2. 选择频率最高者为标准名
3. 调用 `kh_merge_tags(target, sources)` 合并
4. 调用 `kh_log_curation` 记录（action: `merge_tags`）

### 2.2 重复知识检测

调用 `kh_find_similar` 获取疑似重复的知识对（tag 重合率 > 80%）。

对每对：
1. 调用 `kh_read_full` 分别读取两条知识全文
2. 判断：
   - **完全重复**：调用 `kh_merge_knowledge`，保留内容更完整的那条
   - **部分重叠**：调用 `kh_merge_knowledge` 合并互补内容
   - **相似但不同**：保留两条，若有必要调整 tags 以区分
   - **内容冲突**：调用 `kh_create_conflict` 创建冲突报告
3. 调用 `kh_log_curation` 记录决策

---

## 输出汇总

巡检完成后输出：
- 审查了多少条知识，处理了多少条评论
- 合并了多少个 tag
- 合并/归档了多少条知识
- 创建了多少个冲突报告（列出详情供人工排查）
- 权重重算影响了多少条记录
