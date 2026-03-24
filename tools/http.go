package tools

// =====================================================
// http.go - HTTP 请求工具
// 支持 GET/POST/PUT/DELETE 等，返回状态码+响应体
// =====================================================

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type HTTPRequestArgs struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Timeout int               `json:"timeout"`
}

func RunHTTPRequest(args HTTPRequestArgs) string {
	if args.URL == "" {
		return "Error: url is required"
	}
	if args.Method == "" {
		args.Method = "GET"
	}
	args.Method = strings.ToUpper(args.Method)
	if args.Timeout <= 0 {
		args.Timeout = 30
	}

	slog.Info("http_request", "method", args.Method, "url", args.URL, "timeout", args.Timeout)

	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}

	req, err := http.NewRequest(args.Method, args.URL, bodyReader)
	if err != nil {
		return fmt.Sprintf("Error creating request: %v", err)
	}

	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	// 如果有 body 且没设 Content-Type，默认 JSON
	if args.Body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: time.Duration(args.Timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("HTTP %d (read body error: %v)", resp.StatusCode, err)
	}

	// 截断过长响应
	bodyStr := string(respBody)
	if len(bodyStr) > 50000 {
		bodyStr = bodyStr[:50000] + "\n... (truncated)"
	}

	// 构建结果
	result := fmt.Sprintf("HTTP %d %s\n", resp.StatusCode, resp.Status)

	// 显示响应头
	for _, name := range []string{"Content-Type", "Content-Length", "Location", "Set-Cookie"} {
		if v := resp.Header.Get(name); v != "" {
			result += fmt.Sprintf("%s: %s\n", name, v)
		}
	}
	result += "\n" + bodyStr

	// 尝试格式化 JSON
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		var pretty json.RawMessage
		if json.Unmarshal(respBody, &pretty) == nil {
			formatted, err := json.MarshalIndent(pretty, "", "  ")
			if err == nil {
				fmtStr := string(formatted)
				if len(fmtStr) > 50000 {
					fmtStr = fmtStr[:50000] + "\n... (truncated)"
				}
				result = fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, resp.Status, fmtStr)
			}
		}
	}

	return result
}
