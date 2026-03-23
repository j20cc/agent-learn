package main

// =====================================================
// tools.go - 工具定义 + switch-case 分发
// 对应 Python 的 TOOLS 列表和 TOOL_HANDLERS
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

// ---- 工具定义辅助函数 ----

func toolDef(name, desc string, params map[string]any) ToolDef {
	return ToolDef{
		Type:        "function",
		Name:        name,
		Description: desc,
		Parameters:  params,
	}
}

func paramObj(props ...map[string]any) map[string]any {
	properties := map[string]any{}
	var required []string
	for _, p := range props {
		for k, v := range p {
			properties[k] = v
			required = append(required, k)
		}
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func paramObjInt(props ...map[string]any) map[string]any {
	return paramObj(props...)
}

func reqStr(name string) map[string]any {
	return map[string]any{name: map[string]string{"type": "string"}}
}

func reqInt(name string) map[string]any {
	return map[string]any{name: map[string]string{"type": "integer"}}
}

func optStr(name string) map[string]any {
	return map[string]any{name: map[string]string{"type": "string"}}
}

func optInt(name string) map[string]any {
	return map[string]any{name: map[string]string{"type": "integer"}}
}

// AllTools 返回所有 24 个工具的定义
func AllTools() []ToolDef {
	return []ToolDef{
		toolDef("bash", "Run a shell command.", paramObj(reqStr("command"))),

		toolDef("read_file", "Read file contents.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]string{"type": "string"},
				"limit": map[string]string{"type": "integer"},
			},
			"required": []string{"path"},
		}),

		toolDef("write_file", "Write content to file.", paramObj(reqStr("path"), reqStr("content"))),

		toolDef("edit_file", "Replace exact text in file.", paramObj(reqStr("path"), reqStr("old_text"), reqStr("new_text"))),

		toolDef("TodoWrite", "Update task tracking list.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content":    map[string]string{"type": "string"},
							"status":     map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
							"activeForm": map[string]string{"type": "string"},
						},
						"required": []string{"content", "status", "activeForm"},
					},
				},
			},
			"required": []string{"items"},
		}),

		toolDef("task", "Spawn a subagent for isolated exploration or work.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt":     map[string]string{"type": "string"},
				"agent_type": map[string]any{"type": "string", "enum": []string{"Explore", "general-purpose"}},
			},
			"required": []string{"prompt"},
		}),

		toolDef("load_skill", "Load specialized knowledge by name.", paramObj(reqStr("name"))),
		toolDef("compress", "Manually compress conversation context.", paramObj()),

		toolDef("background_run", "Run command in background.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]string{"type": "string"},
				"timeout": map[string]string{"type": "integer"},
			},
			"required": []string{"command"},
		}),

		toolDef("check_background", "Check background task status.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]string{"type": "string"},
			},
		}),

		toolDef("task_create", "Create a persistent file task.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject":     map[string]string{"type": "string"},
				"description": map[string]string{"type": "string"},
			},
			"required": []string{"subject"},
		}),

		toolDef("task_get", "Get task details by ID.", paramObj(reqInt("task_id"))),

		toolDef("task_update", "Update task status or dependencies.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":        map[string]string{"type": "integer"},
				"status":         map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "deleted"}},
				"add_blocked_by": map[string]any{"type": "array", "items": map[string]string{"type": "integer"}},
				"add_blocks":     map[string]any{"type": "array", "items": map[string]string{"type": "integer"}},
			},
			"required": []string{"task_id"},
		}),

		toolDef("task_list", "List all tasks.", paramObj()),

		toolDef("spawn_teammate", "Spawn a persistent autonomous teammate.", paramObj(reqStr("name"), reqStr("role"), reqStr("prompt"))),
		toolDef("list_teammates", "List all teammates.", paramObj()),

		toolDef("send_message", "Send a message to a teammate.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"to":       map[string]string{"type": "string"},
				"content":  map[string]string{"type": "string"},
				"msg_type": map[string]any{"type": "string", "enum": []string{"message", "broadcast", "shutdown_request", "shutdown_response", "plan_approval_response"}},
			},
			"required": []string{"to", "content"},
		}),

		toolDef("read_inbox", "Read and drain the lead's inbox.", paramObj()),
		toolDef("broadcast", "Send message to all teammates.", paramObj(reqStr("content"))),
		toolDef("shutdown_request", "Request a teammate to shut down.", paramObj(reqStr("teammate"))),

		toolDef("plan_approval", "Approve or reject a teammate's plan.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]string{"type": "string"},
				"approve":    map[string]string{"type": "boolean"},
				"feedback":   map[string]string{"type": "string"},
			},
			"required": []string{"request_id", "approve"},
		}),

		toolDef("idle", "Enter idle state.", paramObj()),
		toolDef("claim_task", "Claim a task from the board.", paramObj(reqInt("task_id"))),
	}
}

// ---- 关机/计划审批状态 (对应 Python 的 s10) ----

var shutdownRequests = map[string]map[string]string{}
var planRequests = map[string]map[string]string{}

func handleShutdownRequest(teammate string) string {
	reqID := uuid.New().String()[:8]
	shutdownRequests[reqID] = map[string]string{"target": teammate, "status": "pending"}
	globalBus.Send("lead", teammate, "Please shut down.", "shutdown_request", map[string]any{"request_id": reqID})
	slog.Info("shutdown requested", "request_id", reqID, "teammate", teammate)
	return fmt.Sprintf("Shutdown request %s sent to '%s'", reqID, teammate)
}

func handlePlanReview(requestID string, approve bool, feedback string) string {
	req, ok := planRequests[requestID]
	if !ok {
		return fmt.Sprintf("Error: Unknown plan request_id '%s'", requestID)
	}
	status := "rejected"
	if approve {
		status = "approved"
	}
	req["status"] = status
	globalBus.Send("lead", req["from"], feedback, "plan_approval_response", map[string]any{
		"request_id": requestID, "approve": approve, "feedback": feedback,
	})
	return fmt.Sprintf("Plan %s for '%s'", status, req["from"])
}

// ---- 子 Agent (对应 Python 的 s04) ----

func runSubagent(prompt, agentType string) string {
	slog.Info("subagent spawned", "type", agentType, "prompt_len", len(prompt))

	subTools := []ToolDef{
		toolDef("bash", "Run command.", paramObj(reqStr("command"))),
		toolDef("read_file", "Read file.", paramObj(reqStr("path"))),
	}
	if agentType != "Explore" {
		subTools = append(subTools,
			toolDef("write_file", "Write file.", paramObj(reqStr("path"), reqStr("content"))),
			toolDef("edit_file", "Edit file.", paramObj(reqStr("path"), reqStr("old_text"), reqStr("new_text"))),
		)
	}

	input := []InputItem{
		{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: prompt}}},
	}

	var lastResp *ResponsesResponse
	for i := 0; i < 30; i++ {
		resp, err := CallLLM("", input, subTools)
		if err != nil {
			slog.Error("subagent LLM call failed", "error", err)
			return "(subagent failed)"
		}
		lastResp = resp

		// 把 AI 回复添加到 input
		for _, item := range resp.Output {
			if item.Type == "message" {
				input = append(input, InputItem{
					Type: "message", Role: "assistant", Content: item.Content,
				})
			}
		}

		// 检查是否有工具调用
		hasFuncCalls := false
		for _, item := range resp.Output {
			if item.Type == "function_call" {
				hasFuncCalls = true
				break
			}
		}
		if !hasFuncCalls {
			break
		}

		// 执行工具
		for _, item := range resp.Output {
			if item.Type != "function_call" {
				continue
			}
			output := dispatchBaseTool(item.Name, item.Arguments)
			slog.Info("subagent tool", "tool", item.Name, "output_len", len(output))
			if len(output) > 50000 {
				output = output[:50000]
			}
			input = append(input, InputItem{
				Type: "function_call_output", CallID: item.CallID, Output: output,
			})
		}
	}

	// 提取最终文本
	if lastResp != nil {
		for _, item := range lastResp.Output {
			if item.Type == "message" {
				for _, c := range item.Content {
					if c.Type == "output_text" && c.Text != "" {
						return c.Text
					}
				}
			}
		}
	}
	return "(no summary)"
}

// DispatchTool 主 Agent 的工具分发（大 switch-case）
func DispatchTool(name, argsJSON string) string {
	slog.Info("dispatch tool", "name", name, "args_len", len(argsJSON))

	switch name {
	case "bash":
		var args struct{ Command string `json:"command"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return runBash(args.Command)

	case "read_file":
		var args struct {
			Path  string `json:"path"`
			Limit int    `json:"limit"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return runRead(args.Path, args.Limit)

	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return runWrite(args.Path, args.Content)

	case "edit_file":
		var args struct {
			Path    string `json:"path"`
			OldText string `json:"old_text"`
			NewText string `json:"new_text"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return runEdit(args.Path, args.OldText, args.NewText)

	case "TodoWrite":
		var args struct{ Items []TodoItem `json:"items"` }
		json.Unmarshal([]byte(argsJSON), &args)
		result, err := globalTodo.Update(args.Items)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result

	case "task":
		var args struct {
			Prompt    string `json:"prompt"`
			AgentType string `json:"agent_type"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.AgentType == "" {
			args.AgentType = "Explore"
		}
		return runSubagent(args.Prompt, args.AgentType)

	case "load_skill":
		var args struct{ Name string `json:"name"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return globalSkills.Load(args.Name)

	case "compress":
		return "Compressing..." // 主循环会处理

	case "background_run":
		var args struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return globalBg.Run(args.Command, args.Timeout)

	case "check_background":
		var args struct{ TaskID string `json:"task_id"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return globalBg.Check(args.TaskID)

	case "task_create":
		var args struct {
			Subject     string `json:"subject"`
			Description string `json:"description"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return globalTaskMgr.Create(args.Subject, args.Description)

	case "task_get":
		var args struct{ TaskID int `json:"task_id"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return globalTaskMgr.Get(args.TaskID)

	case "task_update":
		var args struct {
			TaskID     int    `json:"task_id"`
			Status     string `json:"status"`
			AddBlockBy []int  `json:"add_blocked_by"`
			AddBlocks  []int  `json:"add_blocks"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return globalTaskMgr.Update(args.TaskID, args.Status, args.AddBlockBy, args.AddBlocks)

	case "task_list":
		return globalTaskMgr.ListAll()

	case "spawn_teammate":
		var args struct {
			Name   string `json:"name"`
			Role   string `json:"role"`
			Prompt string `json:"prompt"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return globalTeam.Spawn(args.Name, args.Role, args.Prompt)

	case "list_teammates":
		return globalTeam.ListAll()

	case "send_message":
		var args struct {
			To      string `json:"to"`
			Content string `json:"content"`
			MsgType string `json:"msg_type"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		if args.MsgType == "" {
			args.MsgType = "message"
		}
		return globalBus.Send("lead", args.To, args.Content, args.MsgType, nil)

	case "read_inbox":
		msgs := globalBus.ReadInbox("lead")
		data, _ := json.MarshalIndent(msgs, "", "  ")
		return string(data)

	case "broadcast":
		var args struct{ Content string `json:"content"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return globalBus.Broadcast("lead", args.Content, globalTeam.MemberNames())

	case "shutdown_request":
		var args struct{ Teammate string `json:"teammate"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return handleShutdownRequest(args.Teammate)

	case "plan_approval":
		var args struct {
			RequestID string `json:"request_id"`
			Approve   bool   `json:"approve"`
			Feedback  string `json:"feedback"`
		}
		json.Unmarshal([]byte(argsJSON), &args)
		return handlePlanReview(args.RequestID, args.Approve, args.Feedback)

	case "idle":
		return "Lead does not idle."

	case "claim_task":
		var args struct{ TaskID int `json:"task_id"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return globalTaskMgr.Claim(args.TaskID, "lead")

	default:
		slog.Warn("unknown tool", "name", name)
		return fmt.Sprintf("Unknown tool: %s", name)
	}
}
