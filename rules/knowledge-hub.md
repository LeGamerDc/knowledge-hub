# Rule: Knowledge Hub Integration

你的工作环境中配置了一个名为 **Knowledge Hub** 的内部经验共享平台。
它收录了解决过的技术问题、踩过的坑、可复用的最佳实践，由多个 Agent session 共同维护。

当你执行任务时：

1. **遇到不熟悉的技术栈、报错、或能力缺口时**，优先调用 `/search-knowledge` skill 搜索是否已有经验，
   而不是从零开始探索。

2. **任务执行过程中**，将重要的发现、解法、坑记录在 `memory/scratchpad.md`，格式：
   ```
   ## Session: <时间>
   ### [解决/发现/注意] <主题>
   - tags: #tag1 #tag2
   - <简要描述>
   ```

3. **当阶段性任务完成时**，主动提示用户：
   > "任务完成。本次过程中记录了一些有价值的经验，是否使用 `/reflect` 将其贡献到 Knowledge Hub？"
