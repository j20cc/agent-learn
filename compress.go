package main

// =====================================================
// compress.go - 上下文压缩 (对应 Python 的 s06)
// 微压缩 + 自动压缩
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// EstimateTokens 估算 token 数（粗略：JSON 字节数 / 4）
func EstimateTokens(input []InputItem) int {
	data, _ := json.Marshal(input)
	return len(data) / 4
}

// Microcompact 微压缩：把旧的 function_call_output 内容替换成 [cleared]
// 只保留最近 3 个工具结果的完整内容
func Microcompact(input []InputItem) {
	// 找出所有 function_call_output 的索引
	var outputIndices []int
	for i, item := range input {
		if item.Type == "function_call_output" {
			outputIndices = append(outputIndices, i)
		}
	}

	// 如果 <= 3 个，不需要压缩
	if len(outputIndices) <= 3 {
		return
	}

	// 清除除最近 3 个之外的工具输出
	toClean := outputIndices[:len(outputIndices)-3]
	cleaned := 0
	for _, idx := range toClean {
		if len(input[idx].Output) > 100 {
			input[idx].Output = "[cleared]"
			cleaned++
		}
	}

	if cleaned > 0 {
		slog.Info("microcompact done", "cleared", cleaned, "total_outputs", len(outputIndices))
	}
}

// AutoCompact 自动压缩：让 LLM 总结对话，保存完整记录到磁盘
func AutoCompact(input []InputItem) []InputItem {
	slog.Info("auto-compact triggered", "input_count", len(input), "estimated_tokens", EstimateTokens(input))

	// 1. 保存完整的对话记录到 .transcripts/
	transcriptDir := filepath.Join(cfg.WorkDir, ".transcripts")
	os.MkdirAll(transcriptDir, 0o755)
	transcriptPath := filepath.Join(transcriptDir, fmt.Sprintf("transcript_%d.jsonl", time.Now().Unix()))

	f, err := os.Create(transcriptPath)
	if err == nil {
		for _, item := range input {
			line, _ := json.Marshal(item)
			f.Write(line)
			f.WriteString("\n")
		}
		f.Close()
		slog.Info("transcript saved", "path", transcriptPath)
	}

	// 2. 截取对话文本用于总结（最多 80000 字符）
	convData, _ := json.Marshal(input)
	convText := string(convData)
	if len(convText) > 80000 {
		convText = convText[:80000]
	}

	// 3. 调用 LLM 总结
	summaryInput := []InputItem{
		{
			Type: "message",
			Role: "user",
			Content: []ContentPart{
				{Type: "input_text", Text: "Summarize for continuity:\n" + convText},
			},
		},
	}

	resp, err := CallLLM("", summaryInput, nil)
	summary := "(compact failed)"
	if err == nil {
		for _, item := range resp.Output {
			if item.Type == "message" {
				for _, c := range item.Content {
					if c.Type == "output_text" {
						summary = c.Text
					}
				}
			}
		}
	} else {
		slog.Error("auto-compact LLM call failed", "error", err)
	}

	// 4. 用 2 条消息替换整个对话历史
	slog.Info("auto-compact done", "summary_len", len(summary), "transcript", transcriptPath)

	return []InputItem{
		{
			Type: "message",
			Role: "user",
			Content: []ContentPart{
				{Type: "input_text", Text: fmt.Sprintf("[Compressed. Transcript: %s]\n%s", transcriptPath, summary)},
			},
		},
		{
			Type: "message",
			Role: "assistant",
			Content: []ContentPart{
				{Type: "output_text", Text: "Understood. Continuing with summary context."},
			},
		},
	}
}
