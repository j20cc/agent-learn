package main

// =====================================================
// types.go - 所有共享结构体定义
// OpenAI Responses API 数据格式 (POST /v1/responses)
// =====================================================

import "sync"

// ---- OpenAI Responses API: 请求 ----

// ResponsesRequest 是发给 POST /v1/responses 的请求体
type ResponsesRequest struct {
	Model        string        `json:"model"`
	Instructions string        `json:"instructions,omitempty"` // 等同于 system prompt
	Input        []InputItem   `json:"input"`                  // 消息 + 工具结果
	Tools        []ToolDef     `json:"tools,omitempty"`
	MaxTokens    int           `json:"max_output_tokens,omitempty"`
}

// InputItem 可以是消息(message)或工具调用结果(function_call_output)
// 用 Type 字段区分
type InputItem struct {
	// 公共
	Type string `json:"type"` // "message" | "function_call_output"

	// type=message 时使用
	Role    string         `json:"role,omitempty"`    // "user" | "assistant" | "system" | "developer"
	Content []ContentPart  `json:"content,omitempty"` // 消息内容列表

	// type=function_call_output 时使用
	CallID string `json:"call_id,omitempty"` // 对应 function_call 的 id
	Output string `json:"output,omitempty"`  // 工具执行结果
}

// ContentPart 消息中的一个内容块
type ContentPart struct {
	Type string `json:"type"` // "input_text" | "output_text"
	Text string `json:"text"`
}

// ---- OpenAI Responses API: 工具定义 ----

type ToolDef struct {
	Type        string          `json:"type"`                  // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  map[string]any  `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

// ---- OpenAI Responses API: 响应 ----

// ResponsesResponse 是 POST /v1/responses 的响应体
type ResponsesResponse struct {
	ID     string       `json:"id"`
	Status string       `json:"status"` // "completed" | "failed" | "incomplete"
	Output []OutputItem `json:"output"` // 输出项列表
	Usage  *Usage       `json:"usage,omitempty"`
	Error  *APIError    `json:"error,omitempty"`
}

// OutputItem 可以是消息(message)或函数调用(function_call)
type OutputItem struct {
	Type string `json:"type"` // "message" | "function_call"

	// type=message 时
	ID      string        `json:"id,omitempty"`
	Role    string        `json:"role,omitempty"` // "assistant"
	Content []ContentPart `json:"content,omitempty"`

	// type=function_call 时
	CallID    string `json:"call_id,omitempty"`   // 函数调用的唯一 ID
	Name      string `json:"name,omitempty"`      // 函数名
	Arguments string `json:"arguments,omitempty"` // JSON 字符串参数
	Status    string `json:"status,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ---- 会话管理 ----

type Session struct {
	ID       string
	Input    []InputItem // 对话历史（input 数组）
	mu       sync.Mutex
}

// ---- SSE 事件 ----

type SSEEvent struct {
	Type string `json:"type"` // "thinking" | "tool_call" | "tool_result" | "message" | "error" | "done"
	Data any    `json:"data"`
}

// ---- Todo ----

type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`     // "pending" | "in_progress" | "completed"
	ActiveForm string `json:"activeForm"` // 当前正在做什么
}

// ---- 持久化任务 ----

type Task struct {
	ID          int    `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending" | "in_progress" | "completed" | "deleted"
	Owner       string `json:"owner,omitempty"`
	BlockedBy   []int  `json:"blockedBy"`
	Blocks      []int  `json:"blocks"`
}

// ---- 后台任务 ----

type BgTask struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Status  string `json:"status"` // "running" | "completed" | "error"
	Result  string `json:"result,omitempty"`
}

type BgNotification struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Result string `json:"result"`
}

// ---- 消息 ----

type TeamMessage struct {
	Type      string  `json:"type"`      // "message" | "broadcast" | "shutdown_request" | ...
	From      string  `json:"from"`
	Content   string  `json:"content"`
	Timestamp float64 `json:"timestamp"`
	RequestID string  `json:"request_id,omitempty"`
	Approve   *bool   `json:"approve,omitempty"`
	Feedback  string  `json:"feedback,omitempty"`
}

// ---- 队友 ----

type TeamMember struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"` // "working" | "idle" | "shutdown"
}

type TeamConfig struct {
	TeamName string       `json:"team_name"`
	Members  []TeamMember `json:"members"`
}
