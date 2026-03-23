package main

// =====================================================
// tools_base.go - 基础工具：bash / read / write / edit
// 对应 Python 版的 base_tools 部分
// =====================================================

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// safePath 路径沙箱：确保路径不逃逸出工作目录
func safePath(p string) (string, error) {
	abs := p
	if !filepath.IsAbs(p) {
		abs = filepath.Join(cfg.WorkDir, p)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	// 检查是否在工作目录内
	rel, err := filepath.Rel(cfg.WorkDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return abs, nil
}

// runBash 运行 shell 命令，带危险命令拦截和超时
func runBash(command string) string {
	slog.Info("bash exec", "command", command)

	// 危险命令检查
	dangerous := []string{"rm -rf /", "sudo", "shutdown", "reboot", "> /dev/"}
	for _, d := range dangerous {
		if strings.Contains(command, d) {
			slog.Warn("dangerous command blocked", "command", command)
			return "Error: Dangerous command blocked"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cfg.WorkDir

	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))

	if ctx.Err() == context.DeadlineExceeded {
		slog.Warn("bash timeout", "command", command)
		return "Error: Timeout (120s)"
	}
	if err != nil {
		slog.Warn("bash error", "command", command, "error", err)
		if result == "" {
			result = fmt.Sprintf("Error: %v", err)
		}
	}

	// 限制输出长度
	if len(result) > 50000 {
		result = result[:50000]
	}
	if result == "" {
		result = "(no output)"
	}

	slog.Info("bash result", "output_len", len(result))
	return result
}

// runRead 读取文件内容
func runRead(path string, limit int) string {
	slog.Info("read_file", "path", path, "limit", limit)

	fp, err := safePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		slog.Warn("read_file failed", "path", fp, "error", err)
		return fmt.Sprintf("Error: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	if limit > 0 && limit < len(lines) {
		lines = append(lines[:limit], fmt.Sprintf("... (%d more)", len(lines)-limit))
	}

	result := strings.Join(lines, "\n")
	if len(result) > 50000 {
		result = result[:50000]
	}

	slog.Info("read_file ok", "path", fp, "lines", len(lines))
	return result
}

// runWrite 写入文件
func runWrite(path, content string) string {
	slog.Info("write_file", "path", path, "content_len", len(content))

	fp, err := safePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	// 自动创建父目录
	dir := filepath.Dir(fp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("Error: mkdir %v", err)
	}

	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		slog.Warn("write_file failed", "path", fp, "error", err)
		return fmt.Sprintf("Error: %v", err)
	}

	slog.Info("write_file ok", "path", fp, "bytes", len(content))
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path)
}

// runEdit 精确替换文件中的文本（只替换第一个匹配）
func runEdit(path, oldText, newText string) string {
	slog.Info("edit_file", "path", path, "old_len", len(oldText), "new_len", len(newText))

	fp, err := safePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, oldText) {
		slog.Warn("edit_file: text not found", "path", fp)
		return fmt.Sprintf("Error: Text not found in %s", path)
	}

	// 只替换第一个匹配
	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(fp, []byte(newContent), 0o644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	slog.Info("edit_file ok", "path", fp)
	return fmt.Sprintf("Edited %s", path)
}
