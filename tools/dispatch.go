package tools

// =====================================================
// dispatch.go - 工具定义 + switch-case 分发
// 24 个工具的 JSON Schema 定义和主 Agent 分发逻辑
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// ---- 工具定义辅助函数 ----

func ToolDef(name, desc string, params map[string]any) map[string]any {
	return map[string]any{
		"type":        "function",
		"name":        name,
		"description": desc,
		"parameters":  params,
	}
}

func ParamObj(props ...map[string]any) map[string]any {
	properties := map[string]any{}
	required := []string{}
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

func ReqStr(name string) map[string]any {
	return map[string]any{name: map[string]string{"type": "string"}}
}

func ReqInt(name string) map[string]any {
	return map[string]any{name: map[string]string{"type": "integer"}}
}

// AllToolDefs 返回所有 24 个工具的定义（JSON-ready map）
func AllToolDefs() []map[string]any {
	return []map[string]any{
		ToolDef("bash", "Run a shell command.", ParamObj(ReqStr("command"))),

		ToolDef("read_file", "Read file contents.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]string{"type": "string"},
				"limit": map[string]string{"type": "integer"},
			},
			"required": []string{"path"},
		}),

		ToolDef("write_file", "Write content to file.", ParamObj(ReqStr("path"), ReqStr("content"))),
		ToolDef("edit_file", "Replace exact text in file.", ParamObj(ReqStr("path"), ReqStr("old_text"), ReqStr("new_text"))),

		ToolDef("TodoWrite", "Update task tracking list.", map[string]any{
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

		ToolDef("task", "Spawn a subagent for isolated exploration or work.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt":     map[string]string{"type": "string"},
				"agent_type": map[string]any{"type": "string", "enum": []string{"Explore", "general-purpose"}},
			},
			"required": []string{"prompt"},
		}),

		ToolDef("load_skill", "Load specialized knowledge by name.", ParamObj(ReqStr("name"))),
		ToolDef("compress", "Manually compress conversation context.", ParamObj()),

		ToolDef("background_run", "Run command in background.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]string{"type": "string"},
				"timeout": map[string]string{"type": "integer"},
			},
			"required": []string{"command"},
		}),

		ToolDef("check_background", "Check background task status.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]string{"type": "string"},
			},
		}),

		ToolDef("task_create", "Create a persistent file task.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject":     map[string]string{"type": "string"},
				"description": map[string]string{"type": "string"},
			},
			"required": []string{"subject"},
		}),

		ToolDef("task_get", "Get task details by ID.", ParamObj(ReqInt("task_id"))),

		ToolDef("task_update", "Update task status or dependencies.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":        map[string]string{"type": "integer"},
				"status":         map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "deleted"}},
				"add_blocked_by": map[string]any{"type": "array", "items": map[string]string{"type": "integer"}},
				"add_blocks":     map[string]any{"type": "array", "items": map[string]string{"type": "integer"}},
			},
			"required": []string{"task_id"},
		}),

		ToolDef("task_list", "List all tasks.", ParamObj()),
		ToolDef("spawn_teammate", "Spawn a persistent autonomous teammate.", ParamObj(ReqStr("name"), ReqStr("role"), ReqStr("prompt"))),
		ToolDef("list_teammates", "List all teammates.", ParamObj()),

		ToolDef("send_message", "Send a message to a teammate.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"to":       map[string]string{"type": "string"},
				"content":  map[string]string{"type": "string"},
				"msg_type": map[string]any{"type": "string", "enum": []string{"message", "broadcast", "shutdown_request", "shutdown_response", "plan_approval_response"}},
			},
			"required": []string{"to", "content"},
		}),

		ToolDef("read_inbox", "Read and drain the lead's inbox.", ParamObj()),
		ToolDef("broadcast", "Send message to all teammates.", ParamObj(ReqStr("content"))),
		ToolDef("shutdown_request", "Request a teammate to shut down.", ParamObj(ReqStr("teammate"))),

		ToolDef("plan_approval", "Approve or reject a teammate's plan.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]string{"type": "string"},
				"approve":    map[string]string{"type": "boolean"},
				"feedback":   map[string]string{"type": "string"},
			},
			"required": []string{"request_id", "approve"},
		}),

		ToolDef("idle", "Enter idle state.", ParamObj()),
		ToolDef("claim_task", "Claim a task from the board.", ParamObj(ReqInt("task_id"))),
	}
}

// TeammateToolDefs 返回队友可用的工具定义（子集）
func TeammateToolDefs() []map[string]any {
	return []map[string]any{
		ToolDef("bash", "Run command.", ParamObj(ReqStr("command"))),
		ToolDef("read_file", "Read file.", ParamObj(ReqStr("path"))),
		ToolDef("write_file", "Write file.", ParamObj(ReqStr("path"), ReqStr("content"))),
		ToolDef("edit_file", "Edit file.", ParamObj(ReqStr("path"), ReqStr("old_text"), ReqStr("new_text"))),
		ToolDef("send_message", "Send message.", ParamObj(ReqStr("to"), ReqStr("content"))),
		ToolDef("idle", "Signal no more work.", ParamObj()),
		ToolDef("claim_task", "Claim task by ID.", ParamObj(ReqInt("task_id"))),
	}
}

// SubagentToolDefs 返回子 Agent 可用的工具定义
func SubagentToolDefs(agentType string) []map[string]any {
	defs := []map[string]any{
		ToolDef("bash", "Run command.", ParamObj(ReqStr("command"))),
		ToolDef("read_file", "Read file.", ParamObj(ReqStr("path"))),
	}
	if agentType != "Explore" {
		defs = append(defs,
			ToolDef("write_file", "Write file.", ParamObj(ReqStr("path"), ReqStr("content"))),
			ToolDef("edit_file", "Edit file.", ParamObj(ReqStr("path"), ReqStr("old_text"), ReqStr("new_text"))),
		)
	}
	return defs
}

// Registry 持有所有管理器的引用，供工具分发使用
// 避免工具包和主包之间的循环依赖
type Registry struct {
	WorkDir string

	// 主包注入的回调函数
	TodoUpdate       func(items json.RawMessage) string
	RunSubagent      func(prompt, agentType string) string
	SkillLoad        func(name string) string
	BgRun            func(command string, timeout int) string
	BgCheck          func(taskID string) string
	TaskCreate       func(subject, description string) string
	TaskGet          func(id int) string
	TaskUpdate       func(id int, status string, blockedBy, blocks []int) string
	TaskList         func() string
	TaskClaim        func(id int, owner string) string
	SpawnTeammate    func(name, role, prompt string) string
	ListTeammates    func() string
	SendMessage      func(to, content, msgType string) string
	ReadInbox        func() string
	Broadcast        func(content string) string
	ShutdownRequest  func(teammate string) string
	PlanApproval     func(requestID string, approve bool, feedback string) string
}

// Dispatch 主 Agent 的工具分发
func (r *Registry) Dispatch(name, argsJSON string) string {
	slog.Info("dispatch tool", "name", name, "args_len", len(argsJSON))

	switch name {
	case "bash":
		var args struct{ Command string `json:"command"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return RunBash(r.WorkDir, args.Command)

	case "read_file":
		var args struct {
			Path  string `json:"path"`
			Limit int    `json:"limit"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return RunRead(r.WorkDir, args.Path, args.Limit)

	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return RunWrite(r.WorkDir, args.Path, args.Content)

	case "edit_file":
		var args struct {
			Path    string `json:"path"`
			OldText string `json:"old_text"`
			NewText string `json:"new_text"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return RunEdit(r.WorkDir, args.Path, args.OldText, args.NewText)

	case "TodoWrite":
		var args struct{ Items json.RawMessage `json:"items"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.TodoUpdate(args.Items)

	case "task":
		var args struct {
			Prompt    string `json:"prompt"`
			AgentType string `json:"agent_type"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		if args.AgentType == "" {
			args.AgentType = "Explore"
		}
		return r.RunSubagent(args.Prompt, args.AgentType)

	case "load_skill":
		var args struct{ Name string `json:"name"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.SkillLoad(args.Name)

	case "compress":
		return "Compressing..."

	case "background_run":
		var args struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.BgRun(args.Command, args.Timeout)

	case "check_background":
		var args struct{ TaskID string `json:"task_id"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.BgCheck(args.TaskID)

	case "task_create":
		var args struct {
			Subject     string `json:"subject"`
			Description string `json:"description"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.TaskCreate(args.Subject, args.Description)

	case "task_get":
		var args struct{ TaskID int `json:"task_id"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.TaskGet(args.TaskID)

	case "task_update":
		var args struct {
			TaskID     int    `json:"task_id"`
			Status     string `json:"status"`
			AddBlockBy []int  `json:"add_blocked_by"`
			AddBlocks  []int  `json:"add_blocks"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.TaskUpdate(args.TaskID, args.Status, args.AddBlockBy, args.AddBlocks)

	case "task_list":
		return r.TaskList()

	case "spawn_teammate":
		var args struct {
			Name   string `json:"name"`
			Role   string `json:"role"`
			Prompt string `json:"prompt"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.SpawnTeammate(args.Name, args.Role, args.Prompt)

	case "list_teammates":
		return r.ListTeammates()

	case "send_message":
		var args struct {
			To      string `json:"to"`
			Content string `json:"content"`
			MsgType string `json:"msg_type"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		if args.MsgType == "" {
			args.MsgType = "message"
		}
		return r.SendMessage(args.To, args.Content, args.MsgType)

	case "read_inbox":
		return r.ReadInbox()

	case "broadcast":
		var args struct{ Content string `json:"content"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.Broadcast(args.Content)

	case "shutdown_request":
		var args struct{ Teammate string `json:"teammate"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.ShutdownRequest(args.Teammate)

	case "plan_approval":
		var args struct {
			RequestID string `json:"request_id"`
			Approve   bool   `json:"approve"`
			Feedback  string `json:"feedback"`
		}
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.PlanApproval(args.RequestID, args.Approve, args.Feedback)

	case "idle":
		return "Lead does not idle."

	case "claim_task":
		var args struct{ TaskID int `json:"task_id"` }
		jsonUnmarshal([]byte(argsJSON), &args)
		return r.TaskClaim(args.TaskID, "lead")

	default:
		slog.Warn("unknown tool", "name", name)
		return fmt.Sprintf("Unknown tool: %s", name)
	}
}
