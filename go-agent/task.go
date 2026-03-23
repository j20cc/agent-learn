package main

// =====================================================
// task.go - 持久化任务管理 (对应 Python 的 s07)
// 每个任务存储为 .tasks/task_N.json
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type TaskManager struct {
	dir string
	mu  sync.Mutex
}

func NewTaskManager(dir string) *TaskManager {
	os.MkdirAll(dir, 0o755)
	return &TaskManager{dir: dir}
}

// nextID 获取下一个可用 ID
func (m *TaskManager) nextID() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	maxID := 0
	entries, _ := filepath.Glob(filepath.Join(m.dir, "task_*.json"))
	for _, e := range entries {
		base := filepath.Base(e)
		base = strings.TrimPrefix(base, "task_")
		base = strings.TrimSuffix(base, ".json")
		if id, err := strconv.Atoi(base); err == nil && id > maxID {
			maxID = id
		}
	}
	return maxID + 1
}

func (m *TaskManager) load(id int) (*Task, error) {
	path := filepath.Join(m.dir, fmt.Sprintf("task_%d.json", id))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("task %d not found", id)
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (m *TaskManager) save(task *Task) error {
	path := filepath.Join(m.dir, fmt.Sprintf("task_%d.json", task.ID))
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Create 创建新任务
func (m *TaskManager) Create(subject, description string) string {
	id := m.nextID()
	task := &Task{
		ID:          id,
		Subject:     subject,
		Description: description,
		Status:      "pending",
		BlockedBy:   []int{},
		Blocks:      []int{},
	}
	if err := m.save(task); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	slog.Info("task created", "id", id, "subject", subject)
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data)
}

// Get 获取任务详情
func (m *TaskManager) Get(id int) string {
	task, err := m.load(id)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data)
}

// Update 更新任务状态或依赖
func (m *TaskManager) Update(id int, status string, addBlockedBy, addBlocks []int) string {
	task, err := m.load(id)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if status != "" {
		task.Status = status
		slog.Info("task status updated", "id", id, "status", status)

		// 完成时清除其他任务的 blockedBy
		if status == "completed" {
			entries, _ := filepath.Glob(filepath.Join(m.dir, "task_*.json"))
			for _, e := range entries {
				data, _ := os.ReadFile(e)
				var other Task
				if json.Unmarshal(data, &other) == nil {
					newBlocked := []int{}
					for _, b := range other.BlockedBy {
						if b != id {
							newBlocked = append(newBlocked, b)
						}
					}
					if len(newBlocked) != len(other.BlockedBy) {
						other.BlockedBy = newBlocked
						m.save(&other)
					}
				}
			}
		}

		// 删除
		if status == "deleted" {
			path := filepath.Join(m.dir, fmt.Sprintf("task_%d.json", id))
			os.Remove(path)
			return fmt.Sprintf("Task %d deleted", id)
		}
	}

	if len(addBlockedBy) > 0 {
		task.BlockedBy = uniqueAppend(task.BlockedBy, addBlockedBy)
	}
	if len(addBlocks) > 0 {
		task.Blocks = uniqueAppend(task.Blocks, addBlocks)
	}

	m.save(task)
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data)
}

// ListAll 列出所有任务
func (m *TaskManager) ListAll() string {
	entries, _ := filepath.Glob(filepath.Join(m.dir, "task_*.json"))
	if len(entries) == 0 {
		return "No tasks."
	}

	sort.Strings(entries)
	var lines []string
	for _, e := range entries {
		data, err := os.ReadFile(e)
		if err != nil {
			continue
		}
		var task Task
		if json.Unmarshal(data, &task) != nil {
			continue
		}
		marker := "[?]"
		switch task.Status {
		case "pending":
			marker = "[ ]"
		case "in_progress":
			marker = "[>]"
		case "completed":
			marker = "[x]"
		}
		line := fmt.Sprintf("%s #%d: %s", marker, task.ID, task.Subject)
		if task.Owner != "" {
			line += fmt.Sprintf(" @%s", task.Owner)
		}
		if len(task.BlockedBy) > 0 {
			line += fmt.Sprintf(" (blocked by: %v)", task.BlockedBy)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// Claim 认领任务
func (m *TaskManager) Claim(id int, owner string) string {
	task, err := m.load(id)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	task.Owner = owner
	task.Status = "in_progress"
	m.save(task)
	slog.Info("task claimed", "id", id, "owner", owner)
	return fmt.Sprintf("Claimed task #%d for %s", id, owner)
}

func uniqueAppend(base, add []int) []int {
	seen := map[int]bool{}
	for _, v := range base {
		seen[v] = true
	}
	for _, v := range add {
		if !seen[v] {
			base = append(base, v)
			seen[v] = true
		}
	}
	return base
}
