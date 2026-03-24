package main

// =====================================================
// llm.go - OpenAI Responses API 调用
// POST /v1/responses
// =====================================================

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// CallLLM 调用 OpenAI Responses API
func CallLLM(instructions string, input []InputItem, toolDefs []any) (*ResponsesResponse, error) {
	req := ResponsesRequest{
		Model:        cfg.ModelID,
		Instructions: instructions,
		Input:        input,
		Tools:        toolDefs,
		MaxTokens:    8000,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	slog.Info("calling LLM",
		"model", cfg.ModelID,
		"input_count", len(input),
		"tools_count", len(toolDefs),
		"body_bytes", len(body),
	)

	url := cfg.BaseURL + "/v1/responses"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: 120 * time.Second}
	start := time.Now()
	httpResp, err := client.Do(httpReq)
	if err != nil {
		slog.Error("LLM request failed", "error", err, "duration", time.Since(start))
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	slog.Info("LLM response received",
		"status_code", httpResp.StatusCode,
		"body_bytes", len(respBody),
		"duration", time.Since(start),
	)

	if httpResp.StatusCode != http.StatusOK {
		slog.Error("LLM returned non-200", "status", httpResp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("API returned %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp ResponsesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		slog.Error("failed to parse LLM response", "error", err, "body", string(respBody))
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	funcCalls, messages := 0, 0
	for _, item := range resp.Output {
		switch item.Type {
		case "function_call":
			funcCalls++
		case "message":
			messages++
		}
	}
	slog.Info("LLM output parsed",
		"status", resp.Status,
		"function_calls", funcCalls,
		"messages", messages,
		"input_tokens", safeUsageField(resp.Usage, "input"),
		"output_tokens", safeUsageField(resp.Usage, "output"),
	)

	return &resp, nil
}

func safeUsageField(u *Usage, field string) int {
	if u == nil {
		return 0
	}
	switch field {
	case "input":
		return u.InputTokens
	case "output":
		return u.OutputTokens
	default:
		return u.TotalTokens
	}
}
