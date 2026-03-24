package main

// =====================================================
// todo.go - 内存待办事项管理 (对应 Python 的 s03)
// =====================================================

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type TodoManager struct {
	items []TodoItem
	mu    sync.Mutex
}

func NewTodoManager() *TodoManager {
	return &TodoManager{}
}

// Update 更新整个 todo 列表
func (t *TodoManager) Update(items []TodoItem) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	slog.Info("todo update", "count", len(items))

	if len(items) > 20 {
		return "", fmt.Errorf("max 20 todos, got %d", len(items))
	}

	inProgress := 0
	for i, item := range items {
		if item.Content == "" {
			return "", fmt.Errorf("item %d: content required", i)
		}
		switch item.Status {
		case "pending", "in_progress", "completed":
		default:
			return "", fmt.Errorf("item %d: invalid status '%s'", i, item.Status)
		}
		if item.ActiveForm == "" {
			return "", fmt.Errorf("item %d: activeForm required", i)
		}
		if item.Status == "in_progress" {
			inProgress++
		}
	}
	if inProgress > 1 {
		return "", fmt.Errorf("only one in_progress allowed, got %d", inProgress)
	}

	t.items = items
	return t.renderLocked(), nil
}

// Render 渲染 todo 列表为可读文本
func (t *TodoManager) Render() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.renderLocked()
}

func (t *TodoManager) renderLocked() string {
	if len(t.items) == 0 {
		return "No todos."
	}

	var lines []string
	done := 0
	for _, item := range t.items {
		marker := "[?]"
		switch item.Status {
		case "completed":
			marker = "[x]"
			done++
		case "in_progress":
			marker = "[>]"
		case "pending":
			marker = "[ ]"
		}
		line := fmt.Sprintf("%s %s", marker, item.Content)
		if item.Status == "in_progress" {
			line += " <- " + item.ActiveForm
		}
		lines = append(lines, line)
	}
	lines = append(lines, fmt.Sprintf("\n(%d/%d completed)", done, len(t.items)))
	return strings.Join(lines, "\n")
}

// HasOpenItems 是否有未完成项
func (t *TodoManager) HasOpenItems() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range t.items {
		if item.Status != "completed" {
			return true
		}
	}
	return false
}
