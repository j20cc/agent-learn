package main

// =====================================================
// agent.go - Agent 主循环 (对应 Python 的 agent_loop)
// 核心 ReAct 循环：思考→行动→观察→思考...
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// SSEWriter 是 SSE 事件推送函数类型
type SSEWriter func(event SSEEvent)

// AgentLoop Agent 主循环
// session: 当前会话（包含对话历史）
// sse: SSE 推送回调（为 nil 则不推送）
func AgentLoop(session *Session, sse SSEWriter) {
	session.mu.Lock()
	defer session.mu.Unlock()

	roundsWithoutTodo := 0

	for round := 0; round < 50; round++ {
		slog.Info("agent loop round", "round", round, "input_count", len(session.Input))

		// === 第一步：压缩管道 ===
		Microcompact(session.Input)
		tokens := EstimateTokens(session.Input)
		slog.Info("token estimate", "tokens", tokens, "threshold", cfg.TokenThreshold)

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
				InputItem{
					Type: "message", Role: "user",
					Content: []ContentPart{{Type: "input_text", Text: "<background-results>\n" + txt + "</background-results>"}},
				},
				InputItem{
					Type: "message", Role: "assistant",
					Content: []ContentPart{{Type: "output_text", Text: "Noted background results."}},
				},
			)
		}

		// === 第三步：排空收件箱 ===
		inbox := globalBus.ReadInbox("lead")
		if len(inbox) > 0 {
			slog.Info("inbox drained", "count", len(inbox))
			inboxJSON, _ := json.MarshalIndent(inbox, "", "  ")
			session.Input = append(session.Input,
				InputItem{
					Type: "message", Role: "user",
					Content: []ContentPart{{Type: "input_text", Text: "<inbox>" + string(inboxJSON) + "</inbox>"}},
				},
				InputItem{
					Type: "message", Role: "assistant",
					Content: []ContentPart{{Type: "output_text", Text: "Noted inbox messages."}},
				},
			)
		}

		// === 第四步：调用 LLM ===
		if sse != nil {
			sse(SSEEvent{Type: "thinking", Data: map[string]string{"step": "calling LLM"}})
		}

		resp, err := CallLLM(globalSystemPrompt, session.Input, AllTools())
		if err != nil {
			slog.Error("LLM call failed in agent loop", "error", err)
			if sse != nil {
				sse(SSEEvent{Type: "error", Data: map[string]string{"error": err.Error()}})
			}
			return
		}

		// 把 AI 的消息输出加入 input（用于后续轮次的上下文）
		for _, item := range resp.Output {
			if item.Type == "message" {
				session.Input = append(session.Input, InputItem{
					Type: "message", Role: "assistant", Content: item.Content,
				})
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
			// AI 回复了纯文字，任务完成
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
					"name": item.Name,
					"args": json.RawMessage(item.Arguments),
				}})
			}

			if item.Name == "compress" {
				manualCompress = true
			}

			output := DispatchTool(item.Name, item.Arguments)

			slog.Info("tool result", "name", item.Name, "output_len", len(output))

			if sse != nil {
				resultPreview := output
				if len(resultPreview) > 2000 {
					resultPreview = resultPreview[:2000] + "..."
				}
				sse(SSEEvent{Type: "tool_result", Data: map[string]any{
					"name":   item.Name,
					"result": resultPreview,
				}})
			}

			// 把工具结果加入 input
			session.Input = append(session.Input, InputItem{
				Type:   "function_call_output",
				CallID: item.CallID,
				Output: output,
			})

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

		// 手动压缩
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
