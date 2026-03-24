package main

// =====================================================
// team.go - 队友管理 (对应 Python 的 s09/s11)
// 每个队友在独立 goroutine 中运行
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go-agent/tools"
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
	if m := tm.findMember(name); m != nil {
		m.Status = status
		tm.saveConfig()
	}
}

// Spawn 生成一个新队友
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
		tm.config.Members = append(tm.config.Members, TeamMember{Name: name, Role: role, Status: "working"})
	}
	tm.saveConfig()
	tm.mu.Unlock()

	slog.Info("teammate spawned", "name", name, "role", role)
	go tm.teammateLoop(name, role, prompt)
	return fmt.Sprintf("Spawned '%s' (role: %s)", name, role)
}

func (tm *TeammateManager) teammateLoop(name, role, prompt string) {
	slog.Info("teammate loop started", "name", name, "role", role)

	sysPrompt := fmt.Sprintf(
		"You are '%s', role: %s, team: %s, at %s. Use idle when done. You may auto-claim tasks.",
		name, role, tm.config.TeamName, cfg.WorkDir,
	)

	// 队友工具定义
	tmToolDefs := tools.TeammateToolDefs()
	tmToolsAny := make([]any, len(tmToolDefs))
	for i, t := range tmToolDefs {
		tmToolsAny[i] = t
	}

	input := []InputItem{
		{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: prompt}}},
	}

	for {
		// === 工作阶段（最多 50 轮） ===
		idleRequested := false
		for round := 0; round < 50; round++ {
			// 检查收件箱
			for _, msg := range tm.bus.ReadInbox(name) {
				if msg.Type == "shutdown_request" {
					slog.Info("teammate shutdown requested", "name", name)
					tm.setStatus(name, "shutdown")
					return
				}
				msgJSON, _ := json.Marshal(msg)
				input = append(input, InputItem{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: string(msgJSON)}}})
			}

			resp, err := CallLLM(sysPrompt, input, tmToolsAny)
			if err != nil {
				slog.Error("teammate LLM failed", "name", name, "error", err)
				tm.setStatus(name, "shutdown")
				return
			}

			hasFuncCalls := false
			for _, item := range resp.Output {
				switch item.Type {
				case "message":
					input = append(input, InputItem{Type: "message", Role: "assistant", Content: item.Content})
				case "function_call":
					hasFuncCalls = true
					input = append(input, InputItem{
						Type: "function_call", CallID: item.CallID,
						Name: item.Name, Arguments: item.Arguments, Status: item.Status,
					})
				}
			}
			if !hasFuncCalls {
				break
			}

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
					var args struct{ To, Content string }
					json.Unmarshal([]byte(item.Arguments), &args)
					output = tm.bus.Send(name, args.To, args.Content, "message", nil)
				default:
					output = tools.DispatchBaseTool(cfg.WorkDir, item.Name, item.Arguments)
				}
				slog.Info("teammate tool", "name", name, "tool", item.Name, "output_len", len(output))
				input = append(input, InputItem{Type: "function_call_output", CallID: item.CallID, Output: output})
			}
			if idleRequested {
				break
			}
		}

		// === 空闲阶段 ===
		tm.setStatus(name, "idle")
		slog.Info("teammate idle", "name", name)

		resume := false
		for i := 0; i < cfg.IdleTimeout/max(cfg.PollInterval, 1); i++ {
			time.Sleep(time.Duration(cfg.PollInterval) * time.Second)

			inbox := tm.bus.ReadInbox(name)
			if len(inbox) > 0 {
				for _, msg := range inbox {
					if msg.Type == "shutdown_request" {
						tm.setStatus(name, "shutdown")
						return
					}
					msgJSON, _ := json.Marshal(msg)
					input = append(input, InputItem{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: string(msgJSON)}}})
				}
				resume = true
				break
			}

			// 检查未认领任务
			entries, _ := filepath.Glob(filepath.Join(cfg.WorkDir, ".tasks", "task_*.json"))
			for _, e := range entries {
				data, _ := os.ReadFile(e)
				var task Task
				if json.Unmarshal(data, &task) != nil {
					continue
				}
				if task.Status == "pending" && task.Owner == "" && len(task.BlockedBy) == 0 {
					tm.taskMgr.Claim(task.ID, name)
					slog.Info("teammate auto-claimed", "name", name, "task_id", task.ID)

					if len(input) <= 3 {
						input = append([]InputItem{
							{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: fmt.Sprintf("<identity>You are '%s', role: %s, team: %s.</identity>", name, role, tm.config.TeamName)}}},
							{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: fmt.Sprintf("I am %s. Continuing.", name)}}},
						}, input...)
					}
					input = append(input,
						InputItem{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: fmt.Sprintf("<auto-claimed>Task #%d: %s\n%s</auto-claimed>", task.ID, task.Subject, task.Description)}}},
						InputItem{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: fmt.Sprintf("Claimed task #%d. Working on it.", task.ID)}}},
					)
					resume = true
					break
				}
			}
			if resume {
				break
			}
		}

		if !resume {
			slog.Info("teammate idle timeout", "name", name)
			tm.setStatus(name, "shutdown")
			return
		}
		tm.setStatus(name, "working")
	}
}

func (tm *TeammateManager) ListAll() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if len(tm.config.Members) == 0 {
		return "No teammates."
	}
	lines := fmt.Sprintf("Team: %s", tm.config.TeamName)
	for _, m := range tm.config.Members {
		lines += fmt.Sprintf("\n  %s (%s): %s", m.Name, m.Role, m.Status)
	}
	return lines
}

func (tm *TeammateManager) MemberNames() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var names []string
	for _, m := range tm.config.Members {
		names = append(names, m.Name)
	}
	return names
}
