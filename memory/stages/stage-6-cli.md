# Stage 6: CLI 工具

> 目标：实现 `kh` CLI 工具，供人类在 MVP 阶段进行数据审查和工程纠错。

## 前置条件

- Stage 1 完成（生成的 HTTP Client 可用）
- Stage 4 完成（API Server System 域端点可用）

## 任务清单

### 6.1 CLI 框架搭建

- [ ] 创建 `cmd/kh/main.go`
- [ ] 命令行解析（使用标准库 flag 或轻量方案，避免引入重型 CLI 框架）
- [ ] API Server 地址配置（默认 `http://localhost:19820`，可通过 `--server` flag 或 `KH_SERVER` 环境变量配置）
- [ ] 初始化 `pkg/khclient` 生成的 HTTP Client

### 6.2 命令实现

- [ ] `kh status`：系统状态概览（条目数、tag 数、未处理评论数、冲突数）
- [ ] `kh list [--status active|archived] [--tag TAG] [--limit N]`：列出知识条目
- [ ] `kh read <id>`：读取知识全文（Markdown 输出）
- [ ] `kh restore <id>`：恢复归档条目
- [ ] `kh delete <id>`：硬删除（仅 archived 条目，需二次确认 y/N）
- [ ] `kh conflicts [--status open|resolved]`：列出冲突报告
- [ ] `kh resolve <conflict-id> --resolution "说明"`：解决冲突
- [ ] `kh logs [--limit N]`：查看整理日志（默认 20 条）

### 6.3 输出格式

- [ ] 列表类输出使用简洁表格格式（ID 截断显示前 8 位）
- [ ] 全文类输出使用 Markdown 格式
- [ ] 错误信息输出到 stderr

### 6.4 测试

- [ ] 各命令基本功能测试
- [ ] 错误场景：Server 不可达、条目不存在、删除 active 条目

## 交付物

- `cmd/kh/main.go` 完整 CLI 工具
- 8 个命令全部可用
- `kh status` / `kh list` / `kh read` 基本工作流验证通过

## 参考文档

- `docs/specs/vfs-cli.md` — CLI 命令列表与 System API 映射
