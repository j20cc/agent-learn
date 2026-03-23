package main

// =====================================================
// background.go - 后台任务管理 (对应 Python 的 s08)
// 用 goroutine + channel 替代 Python threading + Queue
// =====================================================

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type BackgroundManager struct {
	tasks   map[string]*BgTask
	mu      sync.Mutex
	notifCh chan BgNotification
}

func NewBackgroundManager() *BackgroundManager {
	return &BackgroundManager{
		tasks:   make(map[string]*BgTask),
		notifCh: make(chan BgNotification, 100),
	}
}

// Run 在后台 goroutine 中运行命令
func (bm *BackgroundManager) Run(command string, timeout int) string {
	id := uuid.New().String()[:8]
	if timeout <= 0 {
		timeout = 120
	}

	bm.mu.Lock()
	bm.tasks[id] = &BgTask{
		ID:      id,
		Command: command,
		Status:  "running",
	}
	bm.mu.Unlock()

	slog.Info("background task started", "id", id, "command", command, "timeout", timeout)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = cfg.WorkDir

		output, err := cmd.CombinedOutput()
		result := strings.TrimSpace(string(output))
		status := "completed"

		if ctx.Err() == context.DeadlineExceeded {
			result = "Error: Timeout"
			status = "error"
		} else if err != nil {
			if result == "" {
				result = fmt.Sprintf("Error: %v", err)
			}
			status = "error"
		}

		if len(result) > 50000 {
			result = result[:50000]
		}
		if result == "" {
			result = "(no output)"
		}

		bm.mu.Lock()
		bm.tasks[id].Status = status
		bm.tasks[id].Result = result
		bm.mu.Unlock()

		slog.Info("background task finished", "id", id, "status", status, "output_len", len(result))

		// 发送通知到 channel
		notifResult := result
		if len(notifResult) > 500 {
			notifResult = notifResult[:500]
		}
		bm.notifCh <- BgNotification{
			TaskID: id,
			Status: status,
			Result: notifResult,
		}
	}()

	return fmt.Sprintf("Background task %s started: %s", id, truncate(command, 80))
}

// Check 检查后台任务状态
func (bm *BackgroundManager) Check(taskID string) string {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if taskID != "" {
		t, ok := bm.tasks[taskID]
		if !ok {
			return fmt.Sprintf("Unknown: %s", taskID)
		}
		result := t.Result
		if result == "" {
			result = "(running)"
		}
		return fmt.Sprintf("[%s] %s", t.Status, result)
	}

	// 列出所有
	if len(bm.tasks) == 0 {
		return "No bg tasks."
	}
	var lines []string
	for id, t := range bm.tasks {
		lines = append(lines, fmt.Sprintf("%s: [%s] %s", id, t.Status, truncate(t.Command, 60)))
	}
	return strings.Join(lines, "\n")
}

// Drain 排空通知队列
func (bm *BackgroundManager) Drain() []BgNotification {
	var notifs []BgNotification
	for {
		select {
		case n := <-bm.notifCh:
			notifs = append(notifs, n)
		default:
			return notifs
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
