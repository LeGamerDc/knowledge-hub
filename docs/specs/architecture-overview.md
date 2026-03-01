# Knowledge Hub 架构概览

## 1. 系统边界与分层架构

为了实现高内聚、低耦合，并满足存储复用、终端集成、独立协议等需求，Knowledge Hub 的工程架构划分为以下核心子系统：

```mermaid
graph TD
    subgraph 独立协议层 (Single Source of Truth)
        OpenAPI[OpenAPI 3.0 / Swagger]
    end

    subgraph 核心基础库 (Core Library)
        StorageEngine[pkg/corestore: 存储与分面检索引擎]
    end

    subgraph 服务端 (Server)
        APIServer[cmd/kh-server: HTTP REST API Server]
    end

    subgraph 客户端组 (Clients)
        MCP[cmd/mcp-shim: Agent MCP 接入端]
        CLI[cmd/kh: CLI 工具]
    end

    subgraph AI 交互层 (Agent Workflows)
        Rules[系统 Prompt / Rules]
        Skills[行为流编排 / Skills]
    end

    %% 依赖关系
    OpenAPI -.->|oapi-codegen| APIServer
    OpenAPI -.->|oapi-codegen| MCP
    OpenAPI -.->|oapi-codegen| CLI

    APIServer -->|Go Interface| StorageEngine
    MCP -->|HTTP/JSON| APIServer
    CLI -->|HTTP/JSON| APIServer

    Rules -.->|控制行为| MCP
    Skills -.->|编排工具| MCP
```

## 2. 核心模块说明

### 2.1 协议层：OpenAPI (独立协议)
- **方案抉择**：放弃 gRPC/Protobuf，采用 **OpenAPI 3.0 (YAML)** 作为 API 的单一事实来源。
- **优势**：通过 `oapi-codegen` 自动生成 HTTP Handler 和强类型 Client，消除 Client/Server 数据结构不一致的问题，且对 Bash/CURL 调试极为友好。

> **ADR: gRPC → OpenAPI 变更**
>
> 工程设计初稿选择 gRPC 作为 MCP Shim 与 API Server 间的通信协议。经评估后变更为 OpenAPI 3.0 + HTTP/JSON：
> - **调试友好**：HTTP 请求可直接通过 curl 发送和检查，无需 grpcurl 等专用工具，显著降低开发和排查成本。
> - **等价类型安全**：`oapi-codegen` 从 OpenAPI YAML 生成 Go 的 Server Interface + Client，提供与 Protobuf codegen 等价的强类型保证。
> - **工具链轻量**：无需 protoc 编译器及 Go/gRPC 插件，减少构建依赖。
> - **MVP 场景适配**：单机部署、低并发场景下，gRPC 的 binary 序列化和 HTTP/2 多路复用优势不显著。

### 2.2 核心底层库 (`pkg/corestore`)
- **定位**：无状态、无网络感知的 Go Module。
- **职责**：封装 SQLite (WAL模式)、维护 Tag 倒排索引、执行 Faceted Browsing 算法、支持高级过滤查询。

### 2.3 服务端 (`cmd/kh-server`)
- **定位**：常驻运行的 HTTP API 服务进程，负责桥接网络请求与底层存储。
- **内部架构 (Clean Architecture)**：
  - **Transport 层 (HTTP Handlers)**：由 OpenAPI 工具自动生成（基于 `chi` Router），负责请求参数校验、JSON 序列化与反序列化。
  - **Service 层 (Business Logic)**：
    - 鉴权与路由隔离检查（确保普通 Agent 无法调用 `/admin` 和 `/system` 接口）。
    - 业务逻辑编排：例如，收到新的评论时，触发权重重算逻辑；贡献知识时，进行基础的内容格式校验。
    - DTO (Data Transfer Object) 与 Corestore Model 之间的相互转换。
  - **Repository 层**：直接调用 `pkg/corestore` 暴露的 Go Interface。

### 2.4 Agent 接口层 (`cmd/mcp-shim`)
- 这是一个极轻量级的进程。Claude Code 每个 Session 会 `spawn` 此进程，通过 `stdio` 建立 MCP 连接。此进程内部使用 OpenAPI 生成的 Client，将 MCP 的 Tool Calls 转化为发往 `kh-server` 的 HTTP 请求。

### 2.5 人工审查层 (`cmd/kh`)
- 轻量 CLI 工具，供人类在 MVP 阶段进行数据审查和工程纠错。
- 通过 OpenAPI 生成的 HTTP Client 直接调用 `/api/v1/system/` 端点，提供知识浏览、归档恢复、硬删除、冲突解决等操作。
- **定位说明**：产品设计中人类无需感知系统存在，但 MVP 阶段需要人工参与保证工程质量（审查管理员 Agent 行为、解决冲突报告、清理异常数据）。随着系统成熟，CLI 使用频率将自然降低。

### 2.6 AI 交互层 (Rules & Skills)
- 知识库系统需要依靠 Agent 的行为流来驱动。我们通过定义系统 Prompt 和 Skill 文件来控制“工作 Agent”和“管理员 Agent”的交互范式。（详情见 `docs/specs/agent-workflows.md`）

## 3. 宏观工程目录结构

```text
knowledge-hub/
├── api/
│   └── openapi.yaml              # API 协议的唯一事实来源
├── cmd/
│   ├── kh-server/                # HTTP API Server 的入口
│   ├── kh/                       # CLI 工具入口
│   └── mcp-shim/                 # Agent 交互端入口
├── pkg/
│   ├── corestore/                # 【独立底层核心】存储引擎与检索算法
│   └── khclient/                 # 自动生成的强类型 HTTP 客户端库
├── internal/
│   └── server/                   # kh-server 的内部业务逻辑
│       ├── handlers/             # 自动生成的 HTTP 路由绑定与控制器
│       └── service/              # 鉴权、权重计算、业务组装逻辑
├── rules/                        # Agent System Prompts (.md)
├── skills/                       # Agent 操作编排 (.md)
├── docs/                         # 设计文档与 Specs
├── go.mod
└── go.sum
```