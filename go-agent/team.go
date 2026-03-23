package main

// =====================================================
// team.go - 队友管理 (对应 Python 的 s09/s11)
// 每个队友在独立 goroutine 中运行
// 工作阶段 → 空闲阶段 → 自动认领任务 / 超时关闭
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TeammateManager struct {
	teamDir    string
	configPath string
	config     TeamConfig
	bus        *MessageBus
	taskMgr    *TaskManager
	mu         sync.Mutex
}

func NewTeammateManager(teamDir string, bus *MessageBus, taskMgr *TaskManager) *TeammateManager {
	os.MkdirAll(teamDir, 0o755)
	tm := &TeammateManager{
		teamDir:    teamDir,
		configPath: filepath.Join(teamDir, "config.json"),
		bus:        bus,
		taskMgr:    taskMgr,
	}
	tm.loadConfig()
	return tm
}

func (tm *TeammateManager) loadConfig() {
	data, err := os.ReadFile(tm.configPath)
	if err != nil {
		tm.config = TeamConfig{TeamName: "default", Members: []TeamMember{}}
		return
	}
	json.Unmarshal(data, &tm.config)
}

func (tm *TeammateManager) saveConfig() {
	data, _ := json.MarshalIndent(tm.config, "", "  ")
	os.WriteFile(tm.configPath, data, 0o644)
}

func (tm *TeammateManager) findMember(name string) *TeamMember {
	for i := range tm.config.Members {
		if tm.config.Members[i].Name == name {
			return &tm.config.Members[i]
		}
	}
	return nil
}

func (tm *TeammateManager) setStatus(name, status string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	m := tm.findMember(name)
	if m != nil {
		m.Status = status
		tm.saveConfig()
	}
}

// Spawn 生成一个新队友（或重新启动已有的）
func (tm *TeammateManager) Spawn(name, role, prompt string) string {
	tm.mu.Lock()
	member := tm.findMember(name)
	if member != nil {
		if member.Status != "idle" && member.Status != "shutdown" {
			tm.mu.Unlock()
			return fmt.Sprintf("Error: '%s' is currently %s", name, member.Status)
		}
		member.Status = "working"
		member.Role = role
	} else {
		tm.config.Members = append(tm.config.Members, TeamMember{
			Name:   name,
			Role:   role,
			Status: "working",
		})
	}
	tm.saveConfig()
	tm.mu.Unlock()

	slog.Info("teammate spawned", "name", name, "role", role)

	// 启动独立 goroutine
	go tm.teammateLoop(name, role, prompt)

	return fmt.Sprintf("Spawned '%s' (role: %s)", name, role)
}

// teammateLoop 队友的主循环
func (tm *TeammateManager) teammateLoop(name, role, prompt string) {
	slog.Info("teammate loop started", "name", name, "role", role)

	sysPrompt := fmt.Sprintf(
		"You are '%s', role: %s, team: %s, at %s. Use idle when done with current work. You may auto-claim tasks.",
		name, role, tm.config.TeamName, cfg.WorkDir,
	)

	// 队友可用工具（比主 Agent 少）
	tools := []ToolDef{
		toolDef("bash", "Run command.", paramObj(reqStr("command"))),
		toolDef("read_file", "Read file.", paramObj(reqStr("path"))),
		toolDef("write_file", "Write file.", paramObj(reqStr("path"), reqStr("content"))),
		toolDef("edit_file", "Edit file.", paramObj(reqStr("path"), reqStr("old_text"), reqStr("new_text"))),
		toolDef("send_message", "Send message.", paramObj(reqStr("to"), reqStr("content"))),
		toolDef("idle", "Signal no more work.", paramObj()),
		toolDef("claim_task", "Claim task by ID.", paramObjInt(reqInt("task_id"))),
	}

	input := []InputItem{
		{
			Type: "message",
			Role: "user",
			Content: []ContentPart{
				{Type: "input_text", Text: prompt},
			},
		},
	}

	for {
		// === 工作阶段（最多 50 轮） ===
		idleRequested := false

		for round := 0; round < 50; round++ {
			// 检查收件箱
			inbox := tm.bus.ReadInbox(name)
			for _, msg := range inbox {
				if msg.Type == "shutdown_request" {
					slog.Info("teammate shutdown requested", "name", name)
					tm.setStatus(name, "shutdown")
					return
				}
				msgJSON, _ := json.Marshal(msg)
				input = append(input, InputItem{
					Type: "message",
					Role: "user",
					Content: []ContentPart{
						{Type: "input_text", Text: string(msgJSON)},
					},
				})
			}

			// 调用 LLM
			resp, err := CallLLM(sysPrompt, input, tools)
			if err != nil {
				slog.Error("teammate LLM call failed", "name", name, "error", err)
				tm.setStatus(name, "shutdown")
				return
			}

			// 把 AI 的响应输出作为 assistant 消息加入 input
			for _, item := range resp.Output {
				if item.Type == "message" {
					input = append(input, InputItem{
						Type:    "message",
						Role:    "assistant",
						Content: item.Content,
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

			// 执行工具调用
			for _, item := range resp.Output {
				if item.Type != "function_call" {
					continue
				}

				var output string
				switch item.Name {
				case "idle":
					idleRequested = true
					output = "Entering idle phase."
				case "claim_task":
					var args struct{ TaskID int `json:"task_id"` }
					json.Unmarshal([]byte(item.Arguments), &args)
					output = tm.taskMgr.Claim(args.TaskID, name)
				case "send_message":
					var args struct {
						To      string `json:"to"`
						Content string `json:"content"`
					}
					json.Unmarshal([]byte(item.Arguments), &args)
					output = tm.bus.Send(name, args.To, args.Content, "message", nil)
				default:
					output = dispatchBaseTool(item.Name, item.Arguments)
				}

				slog.Info("teammate tool", "name", name, "tool", item.Name, "output_len", len(output))

				// 把工具结果加入 input
				input = append(input, InputItem{
					Type:   "function_call_output",
					CallID: item.CallID,
					Output: output,
				})
			}

			if idleRequested {
				break
			}
		}

		// === 空闲阶段 ===
		tm.setStatus(name, "idle")
		slog.Info("teammate entering idle", "name", name)

		resume := false
		pollCount := cfg.IdleTimeout / max(cfg.PollInterval, 1)

		for i := 0; i < pollCount; i++ {
			time.Sleep(time.Duration(cfg.PollInterval) * time.Second)

			// 检查收件箱
			inbox := tm.bus.ReadInbox(name)
			if len(inbox) > 0 {
				for _, msg := range inbox {
					if msg.Type == "shutdown_request" {
						slog.Info("teammate shutdown during idle", "name", name)
						tm.setStatus(name, "shutdown")
						return
					}
					msgJSON, _ := json.Marshal(msg)
					input = append(input, InputItem{
						Type: "message",
						Role: "user",
						Content: []ContentPart{
							{Type: "input_text", Text: string(msgJSON)},
						},
					})
				}
				resume = true
				break
			}

			// 检查未认领的任务
			entries, _ := filepath.Glob(filepath.Join(cfg.WorkDir, ".tasks", "task_*.json"))
			for _, e := range entries {
				data, _ := os.ReadFile(e)
				var task Task
				if json.Unmarshal(data, &task) != nil {
					continue
				}
				if task.Status == "pending" && task.Owner == "" && len(task.BlockedBy) == 0 {
					tm.taskMgr.Claim(task.ID, name)
					slog.Info("teammate auto-claimed task", "name", name, "task_id", task.ID)

					// 身份重新注入（上下文压缩后可能丢失）
					if len(input) <= 3 {
						input = append([]InputItem{
							{
								Type: "message", Role: "user",
								Content: []ContentPart{
									{Type: "input_text", Text: fmt.Sprintf("<identity>You are '%s', role: %s, team: %s.</identity>", name, role, tm.config.TeamName)},
								},
							},
							{
								Type: "message", Role: "assistant",
								Content: []ContentPart{
									{Type: "output_text", Text: fmt.Sprintf("I am %s. Continuing.", name)},
								},
							},
						}, input...)
					}

					input = append(input, InputItem{
						Type: "message", Role: "user",
						Content: []ContentPart{
							{Type: "input_text", Text: fmt.Sprintf("<auto-claimed>Task #%d: %s\n%s</auto-claimed>", task.ID, task.Subject, task.Description)},
						},
					})
					input = append(input, InputItem{
						Type: "message", Role: "assistant",
						Content: []ContentPart{
							{Type: "output_text", Text: fmt.Sprintf("Claimed task #%d. Working on it.", task.ID)},
						},
					})
					resume = true
					break
				}
			}
			if resume {
				break
			}
		}

		if !resume {
			slog.Info("teammate idle timeout, shutting down", "name", name)
			tm.setStatus(name, "shutdown")
			return
		}

		tm.setStatus(name, "working")
		slog.Info("teammate resuming work", "name", name)
	}
}

// ListAll 列出所有队友
func (tm *TeammateManager) ListAll() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.config.Members) == 0 {
		return "No teammates."
	}
	lines := []string{fmt.Sprintf("Team: %s", tm.config.TeamName)}
	for _, m := range tm.config.Members {
		lines = append(lines, fmt.Sprintf("  %s (%s): %s", m.Name, m.Role, m.Status))
	}
	return join(lines, "\n")
}

// MemberNames 返回所有队友名称
func (tm *TeammateManager) MemberNames() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var names []string
	for _, m := range tm.config.Members {
		names = append(names, m.Name)
	}
	return names
}

// dispatchBaseTool 分发基础工具（给队友用的简化版）
func dispatchBaseTool(name, argsJSON string) string {
	switch name {
	case "bash":
		var args struct{ Command string `json:"command"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return runBash(args.Command)
	case "read_file":
		var args struct{ Path string `json:"path"` }
		json.Unmarshal([]byte(argsJSON), &args)
		return runRead(args.Path, 0)
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
	default:
		return fmt.Sprintf("Unknown tool: %s", name)
	}
}

func join(lines []string, sep string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += sep
		}
		result += l
	}
	return result
}
