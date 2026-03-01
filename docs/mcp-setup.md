# Knowledge Hub — MCP 配置与启动指南

## 概述

Knowledge Hub 由两个独立进程组成：

| 进程 | 说明 |
|------|------|
| `kh-server` | HTTP API Server，长期运行，管理 SQLite 数据库 |
| `mcp-shim` | MCP stdio 进程，由 Claude Code 按需 spawn，转发请求到 API Server |

## 1. 构建二进制文件

```bash
cd /path/to/knowledge-hub

# 生成代码 + 构建所有二进制
make build

# 或仅构建（若 openapi.yaml 未变更）
go build -o bin/kh-server ./cmd/kh-server
go build -o bin/mcp-shim  ./cmd/mcp-shim
go build -o bin/kh        ./cmd/kh
```

## 2. 启动 API Server

```bash
# 默认端口 :19820，数据库存储在 ./knowledge-hub.db
./bin/kh-server

# 自定义配置
KH_DB_PATH=/var/data/kh.db ./bin/kh-server --port 19820
```

建议通过系统服务（launchd / systemd）管理，保证 API Server 常驻后台。

## 3. Claude Code MCP 配置

### 3.1 工作 Agent 配置（6 个 Tool）

适用于日常任务 Agent，仅暴露知识读写工具。

在项目的 `.mcp.json` 或 `~/.claude/claude_desktop_config.json` 中添加：

```json
{
  "mcpServers": {
    "knowledge-hub": {
      "command": "/path/to/knowledge-hub/bin/mcp-shim",
      "env": {
        "KH_SERVER_URL": "http://localhost:19820",
        "KH_MODE": "worker"
      }
    }
  }
}
```

可用 Tools（6 个）：
- `kh_browse` — Faceted 浏览知识库目录
- `kh_search` — 按关键词 + tags 搜索
- `kh_read_full` — 读取知识全文
- `kh_contribute` — 贡献新知识
- `kh_append_knowledge` — 追加补充/纠错内容
- `kh_comment` — 添加使用反馈评论

### 3.2 管理 Agent 配置（12 个 Tool）

适用于知识整理/管理任务，暴露审查和维护工具。

```json
{
  "mcpServers": {
    "knowledge-hub-admin": {
      "command": "/path/to/knowledge-hub/bin/mcp-shim",
      "env": {
        "KH_SERVER_URL": "http://localhost:19820",
        "KH_MODE": "admin"
      }
    }
  }
}
```

可用 Tools（12 个）：
- `kh_list_flagged` — 获取待审查知识列表
- `kh_tag_health` — Tag 健康报告
- `kh_find_similar` — 疑似重复检测
- `kh_get_review` — 获取知识审查详情（全文 + 未处理评论）
- `kh_update_knowledge` — 全量更新知识（全文重构用）
- `kh_archive` — 归档（软删除）
- `kh_mark_processed` — 标记评论已处理
- `kh_merge_tags` — 合并同义 Tag
- `kh_merge_knowledge` — 合并重复知识
- `kh_create_conflict` — 创建冲突报告
- `kh_log_curation` — 写入整理日志
- `kh_recalculate_weights` — 批量重算权重

### 3.3 全工具配置（调试用）

不设置 `KH_MODE` 或设为空，mcp-shim 注册全部 18 个工具：

```json
{
  "mcpServers": {
    "knowledge-hub-all": {
      "command": "/path/to/knowledge-hub/bin/mcp-shim",
      "env": {
        "KH_SERVER_URL": "http://localhost:19820"
      }
    }
  }
}
```

## 4. Rules 与 Skills 配置

### 4.1 Rules（CLAUDE.md 或项目 Rules 文件）

将 `rules/knowledge-hub.md` 的内容追加到项目的 `CLAUDE.md` 或
在 Claude Code Settings → Rules 中引用此文件，使 Agent 感知 Knowledge Hub 的存在。

### 4.2 Skills（自定义命令）

将 `skills/` 目录下的文件注册为 Claude Code 自定义命令：

| 命令 | 文件 | 说明 |
|------|------|------|
| `/search-knowledge` | `skills/search-knowledge.md` | 渐进式搜索知识库 |
| `/contribute-knowledge` | `skills/contribute-knowledge.md` | 贡献经验到知识库 |
| `/reflect` | `skills/reflect.md` | 总结经验 + 评价已用知识 |
| `/admin-inspect` | `skills/admin-inspect.md` | 管理员全库巡检 |

在 `.claude/commands/` 目录中创建对应文件（内容与 `skills/` 相同），
Claude Code 会自动识别为 slash command。

## 5. CLI 工具

```bash
# 查看系统状态
./bin/kh status

# 列出知识条目
./bin/kh list

# 读取某条知识
./bin/kh read <id>

# 恢复归档知识
./bin/kh restore <id>

# 硬删除（需二次确认）
./bin/kh delete <id>

# 查看冲突报告
./bin/kh conflicts

# 解决冲突
./bin/kh resolve <conflict-id> --resolution "解决方案描述"

# 查看整理日志
./bin/kh logs
```

默认连接 `http://localhost:19820`，可通过 `--server <URL>` 或 `$KH_SERVER` 覆盖。

## 6. 快速验证

```bash
# 1. 启动 API Server（后台）
./bin/kh-server &

# 2. 验证 API 可用
curl -s http://localhost:19820/api/v1/system/status | jq .

# 3. 验证 CLI
./bin/kh status

# 4. 测试 MCP（需 claude code 环境）
KH_SERVER_URL=http://localhost:19820 KH_MODE=worker ./bin/mcp-shim
```
