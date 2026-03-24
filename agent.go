package main

// =====================================================
// agent.go - Agent 主循环 + 子Agent
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"go-agent/tools"
)

type SSEWriter func(event SSEEvent)

// AgentLoop Agent 主循环
func AgentLoop(session *Session, sse SSEWriter) {
	session.mu.Lock()
	defer session.mu.Unlock()

	roundsWithoutTodo := 0
	allToolDefs := tools.AllToolDefs()

	for round := 0; round < 50; round++ {
		slog.Info("agent loop round", "round", round, "input_count", len(session.Input))

		// === 第一步：压缩管道 ===
		Microcompact(session.Input)
		tokens := EstimateTokens(session.Input)
		if tokens > cfg.TokenThreshold {
			slog.Info("auto-compact triggered")
			if sse != nil {
				sse(SSEEvent{Type: "thinking", Data: map[string]string{"step": "auto-compact"}})
			}
			session.Input = AutoCompact(session.Input)
		}

		// === 第二步：排空后台通知 ===
		notifs := globalBg.Drain()
		if len(notifs) > 0 {
			slog.Info("bg notifications drained", "count", len(notifs))
			txt := ""
			for _, n := range notifs {
				txt += fmt.Sprintf("[bg:%s] %s: %s\n", n.TaskID, n.Status, n.Result)
			}
			session.Input = append(session.Input,
				InputItem{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: "<background-results>\n" + txt + "</background-results>"}}},
				InputItem{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "Noted background results."}}},
			)
		}

		// === 第三步：排空收件箱 ===
		inbox := globalBus.ReadInbox("lead")
		if len(inbox) > 0 {
			slog.Info("inbox drained", "count", len(inbox))
			inboxJSON, _ := json.MarshalIndent(inbox, "", "  ")
			session.Input = append(session.Input,
				InputItem{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: "<inbox>" + string(inboxJSON) + "</inbox>"}}},
				InputItem{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "Noted inbox messages."}}},
			)
		}

		// === 第四步：调用 LLM ===
		if sse != nil {
			sse(SSEEvent{Type: "thinking", Data: map[string]string{"step": "calling LLM"}})
		}

		// 将 []map[string]any 转换为 []any
		toolsAny := make([]any, len(allToolDefs))
		for i, t := range allToolDefs {
			toolsAny[i] = t
		}

		resp, err := CallLLM(globalSystemPrompt, session.Input, toolsAny)
		if err != nil {
			slog.Error("LLM call failed", "error", err)
			if sse != nil {
				sse(SSEEvent{Type: "error", Data: map[string]string{"error": err.Error()}})
			}
			return
		}

		// 把 AI 的消息加入 input
		for _, item := range resp.Output {
			if item.Type == "message" {
				session.Input = append(session.Input, InputItem{Type: "message", Role: "assistant", Content: item.Content})
			}
		}

		// === 第五步：检查是否有工具调用 ===
		hasFuncCalls := false
		for _, item := range resp.Output {
			if item.Type == "function_call" {
				hasFuncCalls = true
				break
			}
		}

		if !hasFuncCalls {
			for _, item := range resp.Output {
				if item.Type == "message" {
					for _, c := range item.Content {
						if c.Type == "output_text" {
							slog.Info("agent final reply", "text_len", len(c.Text))
							if sse != nil {
								sse(SSEEvent{Type: "message", Data: map[string]string{"content": c.Text}})
							}
						}
					}
				}
			}
			return
		}

		// === 第六步：执行工具调用 ===
		usedTodo := false
		manualCompress := false

		for _, item := range resp.Output {
			if item.Type != "function_call" {
				continue
			}

			slog.Info("executing tool", "name", item.Name, "call_id", item.CallID)

			if sse != nil {
				sse(SSEEvent{Type: "tool_call", Data: map[string]any{
					"name": item.Name, "args": json.RawMessage(item.Arguments),
				}})
			}

			if item.Name == "compress" {
				manualCompress = true
			}

			output := toolRegistry.Dispatch(item.Name, item.Arguments)

			slog.Info("tool result", "name", item.Name, "output_len", len(output))

			if sse != nil {
				preview := output
				if len(preview) > 2000 {
					preview = preview[:2000] + "..."
				}
				sse(SSEEvent{Type: "tool_result", Data: map[string]any{"name": item.Name, "result": preview}})
			}

			session.Input = append(session.Input, InputItem{Type: "function_call_output", CallID: item.CallID, Output: output})

			if item.Name == "TodoWrite" {
				usedTodo = true
			}
		}

		// === 第七步：Todo 提醒 ===
		if usedTodo {
			roundsWithoutTodo = 0
		} else {
			roundsWithoutTodo++
		}
		if globalTodo.HasOpenItems() && roundsWithoutTodo >= 3 {
			slog.Info("todo reminder injected")
			session.Input = append(session.Input, InputItem{
				Type: "message", Role: "user",
				Content: []ContentPart{{Type: "input_text", Text: "<reminder>Update your todos.</reminder>"}},
			})
		}

		if manualCompress {
			slog.Info("manual compact triggered")
			session.Input = AutoCompact(session.Input)
		}
	}

	slog.Warn("agent loop hit max rounds (50)")
	if sse != nil {
		sse(SSEEvent{Type: "error", Data: map[string]string{"error": "Agent hit max rounds (50)"}})
	}
}

// runSubagent 子 Agent（独立循环）
func runSubagent(prompt, agentType string) string {
	slog.Info("subagent spawned", "type", agentType, "prompt_len", len(prompt))

	subToolDefs := tools.SubagentToolDefs(agentType)
	subToolsAny := make([]any, len(subToolDefs))
	for i, t := range subToolDefs {
		subToolsAny[i] = t
	}

	input := []InputItem{
		{Type: "message", Role: "user", Content: []ContentPart{{Type: "input_text", Text: prompt}}},
	}

	var lastResp *ResponsesResponse
	for i := 0; i < 30; i++ {
		resp, err := CallLLM("", input, subToolsAny)
		if err != nil {
			slog.Error("subagent LLM call failed", "error", err)
			return "(subagent failed)"
		}
		lastResp = resp

		for _, item := range resp.Output {
			if item.Type == "message" {
				input = append(input, InputItem{Type: "message", Role: "assistant", Content: item.Content})
			}
		}

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

		for _, item := range resp.Output {
			if item.Type != "function_call" {
				continue
			}
			output := tools.DispatchBaseTool(cfg.WorkDir, item.Name, item.Arguments)
			slog.Info("subagent tool", "tool", item.Name, "output_len", len(output))
			if len(output) > 50000 {
				output = output[:50000]
			}
			input = append(input, InputItem{Type: "function_call_output", CallID: item.CallID, Output: output})
		}
	}

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
