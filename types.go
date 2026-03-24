package main

// =====================================================
// types.go - 所有共享结构体定义
// OpenAI Responses API 数据格式 (POST /v1/responses)
// =====================================================

import "sync"

// ---- OpenAI Responses API: 请求 ----

type ResponsesRequest struct {
	Model        string      `json:"model"`
	Instructions string      `json:"instructions,omitempty"`
	Input        []InputItem `json:"input"`
	Tools        []any       `json:"tools,omitempty"` // tools 包返回 []map[string]any
	MaxTokens    int         `json:"max_output_tokens,omitempty"`
}

// InputItem 可以是 message / function_call / function_call_output
type InputItem struct {
	Type    string        `json:"type"`
	Role    string        `json:"role,omitempty"`
	Content []ContentPart `json:"content,omitempty"`
	CallID  string        `json:"call_id,omitempty"`
	Output  string        `json:"output,omitempty"`
	// type=function_call 时使用
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Status    string `json:"status,omitempty"`
}

type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ---- OpenAI Responses API: 响应 ----

type ResponsesResponse struct {
	ID     string       `json:"id"`
	Status string       `json:"status"`
	Output []OutputItem `json:"output"`
	Usage  *Usage       `json:"usage,omitempty"`
	Error  *APIError    `json:"error,omitempty"`
}

type OutputItem struct {
	Type      string        `json:"type"`
	ID        string        `json:"id,omitempty"`
	Role      string        `json:"role,omitempty"`
	Content   []ContentPart `json:"content,omitempty"`
	CallID    string        `json:"call_id,omitempty"`
	Name      string        `json:"name,omitempty"`
	Arguments string        `json:"arguments,omitempty"`
	Status    string        `json:"status,omitempty"`
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

// ---- 会话 ----

type Session struct {
	ID    string
	Input []InputItem
	mu    sync.Mutex
}

// ---- SSE 事件 ----

type SSEEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// ---- Todo ----

type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

// ---- 持久化任务 ----

type Task struct {
	ID          int    `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Owner       string `json:"owner,omitempty"`
	BlockedBy   []int  `json:"blockedBy"`
	Blocks      []int  `json:"blocks"`
}

// ---- 后台任务 ----

type BgTask struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Status  string `json:"status"`
	Result  string `json:"result,omitempty"`
}

type BgNotification struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Result string `json:"result"`
}

// ---- 消息 ----

type TeamMessage struct {
	Type      string  `json:"type"`
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
	Status string `json:"status"`
}

type TeamConfig struct {
	TeamName string       `json:"team_name"`
	Members  []TeamMember `json:"members"`
}
