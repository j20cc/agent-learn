# Go Agent — AI 编程助手

基于 Go + Gin + SSE 的 AI Agent 服务器。使用 OpenAI Responses API，支持工具调用、子 Agent、队友协作、后台任务等完整 Agent 能力。

从 [s_full.py](../s_full.py) 完整移植。

---

## 快速开始

```bash
# 1. 进入目录
cd go-agent

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，填入你的 API Key

# 3. 运行
go run .

# 4. 测试
curl -N -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"session_id":"test1","message":"列出当前目录的文件"}'
```

---

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/chat` | 发送消息，返回 SSE 事件流 |
| `GET` | `/tasks` | 查看持久化任务列表 |
| `GET` | `/team` | 查看队友状态 |
| `GET` | `/inbox` | 查看 lead 收件箱 |
| `POST` | `/compact` | 手动压缩会话上下文 |

### POST /chat 请求

```json
{
  "session_id": "my-session",
  "message": "帮我创建一个 hello.py"
}
```

### SSE 事件流

返回的事件按以下顺序推送：

```
event: thinking      // Agent 正在做预处理
data: {"type":"thinking","data":{"step":"calling LLM"}}

event: tool_call     // AI 决定调用工具
data: {"type":"tool_call","data":{"name":"bash","args":{"command":"ls"}}}

event: tool_result   // 工具执行结果
data: {"type":"tool_result","data":{"name":"bash","result":"file1.txt\nfile2.txt"}}

event: message       // AI 最终回复
data: {"type":"message","data":{"content":"已完成..."}}

event: done          // 流结束
data: {"type":"done","data":{}}
```

---

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `OPENAI_API_KEY` | ✅ | — | API 密钥 |
| `OPENAI_BASE_URL` | — | `https://api.openai.com` | API 地址（兼容其他 OpenAI 兼容服务） |
| `MODEL_ID` | — | `gpt-4o` | 模型名称 |
| `PORT` | — | `8080` | 服务端口 |
| `WORKDIR` | — | 当前目录 | 工作目录（文件操作的沙箱根目录） |

---

## 原理与流程

### 核心思想：ReAct 循环

Agent 的本质是一个 **"思考→行动→观察"** 的循环：

```
用户说话 ──▶ Agent（AI 大脑）──选工具──▶ 执行工具 ──返回结果──▶ Agent 再想
                                                                   ↓
                                                            继续选工具 / 给出回复
```

AI 模型不会直接执行任何操作，它只是"决定用哪个工具、传什么参数"，然后由 Go 代码实际执行。

### 完整流程图

```
用户 POST /chat ──▶ Gin 路由
                      │
                      ▼
              ┌── AgentLoop ──────────────────────────────┐
              │                                            │
              │  每轮循环:                                  │
              │                                            │
              │  1. 微压缩                                  │
              │     旧的工具返回结果 → "[cleared]"           │
              │     只保留最近 3 个结果的完整内容             │
              │                                            │
              │  2. 自动压缩（如果 token > 10万）            │
              │     完整对话 → 保存到 .transcripts/          │
              │     调 LLM 总结 → 替换整个对话历史           │
              │                                            │
              │  3. 排空后台通知                             │
              │     goroutine 执行完的命令 → 注入对话        │
              │                                            │
              │  4. 排空收件箱                               │
              │     队友发来的消息 → 注入对话                 │
              │                                            │
              │  5. 调用 LLM                                │
              │     POST /v1/responses ──▶ OpenAI           │
              │     ◀── 返回 output 数组                    │
              │                                            │
              │  6. 检查 output:                             │
              │     ├── 只有 message? → SSE 推送 → 结束     │
              │     └── 有 function_call?                   │
              │           ├── switch(name) 执行工具         │
              │           ├── SSE 推送 tool_call            │
              │           ├── SSE 推送 tool_result          │
              │           └── 结果 → 放回 input → 下一轮    │
              │                                            │
              │  7. Todo 提醒（连续 3 轮没更新就提醒）       │
              │                                            │
              └── 循环，直到 AI 不再调用工具 ──────────────┘
                      │
                      ▼
              SSE 推送 done 事件 ──▶ 前端
```

### OpenAI Responses API 交互格式

与传统的 Chat Completions 不同，Responses API 使用 `input` 数组（而非 `messages`） ：

```
请求: POST /v1/responses
{
  "model": "gpt-4o",
  "instructions": "你是一个编程 Agent...",   ← 类似 system prompt
  "input": [                                 ← 对话历史
    { "type": "message", "role": "user",
      "content": [{"type":"input_text","text":"帮我列出文件"}] },
    { "type": "function_call_output",        ← 上一轮的工具结果
      "call_id": "call_abc",
      "output": "file1.txt\nfile2.txt" }
  ],
  "tools": [                                 ← 工具定义
    { "type":"function", "name":"bash", ... }
  ]
}

响应:
{
  "output": [                                ← AI 的输出
    { "type": "function_call",               ← AI 要调用工具
      "call_id": "call_xyz",
      "name": "write_file",
      "arguments": "{\"path\":\"hello.py\",...}" },
    { "type": "message",                     ← AI 的文字回复
      "role": "assistant",
      "content": [{"type":"output_text","text":"已创建文件"}] }
  ]
}
```

### 24 个工具一览

| 类别 | 工具 | 说明 |
|------|------|------|
| **基础** | `bash` `read_file` `write_file` `edit_file` | 命令执行、文件读写编辑 |
| **待办** | `TodoWrite` | 内存中的任务清单 |
| **子Agent** | `task` | 派遣独立的子 Agent 做隔离任务 |
| **技能** | `load_skill` | 加载预写的知识文档 |
| **压缩** | `compress` | 手动压缩上下文 |
| **后台** | `background_run` `check_background` | 异步命令执行 |
| **任务** | `task_create` `task_get` `task_update` `task_list` `claim_task` | 持久化任务管理（存文件） |
| **团队** | `spawn_teammate` `list_teammates` | 生成/管理 AI 队友 |
| **通信** | `send_message` `read_inbox` `broadcast` | 消息收发 |
| **管控** | `shutdown_request` `plan_approval` `idle` | 关机协议、计划审批 |

### 目录结构

```
go-agent/
├── main.go           # Gin 路由、配置、入口
├── agent.go          # Agent 主循环
├── llm.go            # OpenAI Responses API 调用
├── types.go          # 所有结构体
├── tools.go          # 工具定义 + switch-case 分发
├── tools_base.go     # bash / read / write / edit
├── todo.go           # 待办管理
├── task.go           # 持久化任务
├── skill.go          # 技能加载
├── compress.go       # 上下文压缩
├── background.go     # 后台任务（goroutine + channel）
├── message.go        # 消息总线
├── team.go           # 队友管理
├── .env.example      # 环境变量示例
└── go.mod / go.sum

运行时生成:
├── .tasks/           # 持久化任务 JSON
├── .team/            # 团队配置 + 收件箱
│   ├── config.json
│   └── inbox/
└── .transcripts/     # 压缩前的完整对话记录
```
