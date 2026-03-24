package tools

// =====================================================
// helpers.go - 包内辅助函数
// =====================================================

import "encoding/json"

// jsonUnmarshal 包内用的 json 解析（忽略错误）
func jsonUnmarshal(data []byte, v any) {
	json.Unmarshal(data, v)
}

// Truncate 截断字符串
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
