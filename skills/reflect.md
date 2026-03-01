# Skill: Reflect & Contribute

经验总结与知识贡献。当用户输入 `/reflect` 时触发。

在一个独立的 subagent 中执行以下流程，基于 `memory/scratchpad.md` 进行深度整理。

## 步骤

### Phase 1：评价已使用的知识

1. 读取 `memory/scratchpad.md`，识别本次任务中调用过哪些 Knowledge Hub 条目（scratchpad 中有记录）。

2. 对每条使用过的知识，调用 `kh_comment` 提交反馈：
   - `type`：根据实际效果选择 `success` / `failure` / `supplement` / `correction`
   - `reasoning`（必填）：具体说明为什么——你在什么场景用了它，结果如何，证据是什么
   - `scenario`：任务的简要描述

3. 若有补充内容，调用 `kh_append_knowledge` 追加到原条目（不替换原文）：
   ```
   > **[Supplement/Correction] <时间>**
   > <具体补充或纠错内容>
   ```

### Phase 2：贡献新知识

4. 从 scratchpad 中识别可贡献的新知识候选（解决了新问题、发现了新坑、有可复用的经验）。

5. 对每条候选，按 `/contribute-knowledge` skill 的流程整理并展示给用户确认。

6. 用户确认后调用 `kh_contribute` 提交。

### Phase 3：清理 scratchpad

7. 清除 `memory/scratchpad.md` 中已处理的条目（或标记为 `[已贡献]`），保持 scratchpad 精简。

## 输出

反馈完成后汇总：
- 对多少条已有知识添加了反馈
- 贡献了多少条新知识
- 有哪些 Hub 中未覆盖的主题（供用户了解知识库盲点）
