# CLI 工具设计 (`kh`)

> **定位**：MVP 阶段的人工审查与纠错工具。产品设计中人类无需感知 Knowledge Hub 的存在，但 MVP 早期需要人工参与保证工程质量——审查管理员 Agent 的整理行为、解决 Agent 无法判定的冲突报告、清理异常数据。随着系统成熟和整理 Agent 可靠性提升，CLI 使用频率将自然降低。

## 1. 技术实现

`kh` 是一个独立的命令行二进制（`cmd/kh/main.go`），内部使用 `oapi-codegen` 生成的强类型 HTTP Client（`pkg/khclient`）调用 API Server 的 `/api/v1/system/` 端点。无状态，不需要守护进程。

```
kh <command> [flags]  -->  HTTP/JSON  -->  kh-server (/api/v1/system/*)
```

## 2. 命令列表

### 2.1 系统状态

```bash
kh status
```

查看知识库概览：总条目数（active/archived）、Tag 数量、未处理评论数、待解决冲突数。

### 2.2 列出知识条目

```bash
kh list [--status active|archived] [--tag TAG] [--limit N]
```

列出知识条目（ID、标题、状态、权重、Tag 列表）。默认只显示 active 条目，`--status archived` 可查看已归档条目供审查。

### 2.3 读取知识全文

```bash
kh read <id>
```

输出知识条目的全文（Markdown 格式），包含评论概要。

### 2.4 恢复已归档条目

```bash
kh restore <id>
```

将 archived 状态的条目恢复为 active。用于撤销管理员 Agent 的误操作。

### 2.5 硬删除

```bash
kh delete <id>
```

从数据库物理删除条目。执行前需二次确认（输出条目标题，要求 `y/N` 确认）。用于清除敏感信息或释放存储空间。仅可删除 archived 状态的条目（active 条目需先归档）。

### 2.6 触发管理员巡检

```bash
kh admin-sync
```

手动触发管理员 Agent 的 `admin-inspect` 巡检流程。未来可配置为 cron 定期执行。

### 2.7 查看冲突报告

```bash
kh conflicts [--status open|resolved]
```

列出冲突报告（ID、类型、涉及知识条目、状态）。默认只显示 open 状态。冲突报告由管理员 Agent 在无法判定矛盾时创建，需人工介入解决。

### 2.8 解决冲突

```bash
kh resolve <conflict-id> --resolution "解决说明"
```

将冲突报告标记为 resolved，并记录解决方案。

### 2.9 查看整理日志

```bash
kh logs [--limit N]
```

查看管理员 Agent 的整理操作日志（时间、操作类型、目标、说明）。默认显示最近 20 条。用于审计管理员 Agent 的行为是否合理。

## 3. 与 System API 的映射

| CLI 命令 | HTTP 端点 |
|---------|----------|
| `kh status` | `GET /api/v1/system/status` |
| `kh list` | `GET /api/v1/system/knowledge` |
| `kh read <id>` | `GET /api/v1/system/knowledge/{id}` |
| `kh restore <id>` | `POST /api/v1/system/knowledge/{id}/restore` |
| `kh delete <id>` | `DELETE /api/v1/system/knowledge/{id}` |
| `kh conflicts` | `GET /api/v1/system/conflicts` |
| `kh resolve <id>` | `POST /api/v1/system/conflicts/{id}/resolve` |
| `kh logs` | `GET /api/v1/system/curation-logs` |
