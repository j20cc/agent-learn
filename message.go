package main

// =====================================================
// message.go - 消息总线 (对应 Python 的 s09)
// 基于文件的收件箱系统 (.team/inbox/NAME.jsonl)
// =====================================================

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type MessageBus struct {
	inboxDir string
	mu       sync.Mutex
}

func NewMessageBus(inboxDir string) *MessageBus {
	os.MkdirAll(inboxDir, 0o755)
	return &MessageBus{inboxDir: inboxDir}
}

// Send 发送消息给指定用户
func (mb *MessageBus) Send(sender, to, content, msgType string, extra map[string]any) string {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	msg := TeamMessage{
		Type:      msgType,
		From:      sender,
		Content:   content,
		Timestamp: float64(time.Now().Unix()),
	}

	// 应用 extra 字段
	if v, ok := extra["request_id"]; ok {
		msg.RequestID, _ = v.(string)
	}

	data, _ := json.Marshal(msg)
	path := filepath.Join(mb.inboxDir, to+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer f.Close()

	f.WriteString(string(data) + "\n")

	slog.Info("message sent", "from", sender, "to", to, "type", msgType)
	return fmt.Sprintf("Sent %s to %s", msgType, to)
}

// ReadInbox 读取并清空指定用户的收件箱
func (mb *MessageBus) ReadInbox(name string) []TeamMessage {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	path := filepath.Join(mb.inboxDir, name+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var msgs []TeamMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var msg TeamMessage
		if json.Unmarshal([]byte(line), &msg) == nil {
			msgs = append(msgs, msg)
		}
	}
	f.Close()

	// 清空文件
	os.WriteFile(path, []byte(""), 0o644)

	if len(msgs) > 0 {
		slog.Info("inbox read", "name", name, "count", len(msgs))
	}
	return msgs
}

// Broadcast 广播消息给所有队友（排除发送者）
func (mb *MessageBus) Broadcast(sender, content string, names []string) string {
	count := 0
	for _, name := range names {
		if name != sender {
			mb.Send(sender, name, content, "broadcast", nil)
			count++
		}
	}
	slog.Info("broadcast sent", "from", sender, "count", count)
	return fmt.Sprintf("Broadcast to %d teammates", count)
}
