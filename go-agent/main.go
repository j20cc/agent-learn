package main

// =====================================================
// main.go - Gin 路由、SSE 端点、启动入口
// =====================================================

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// ---- 全局配置 ----

type Config struct {
	BaseURL        string
	APIKey         string
	ModelID        string
	WorkDir        string
	TokenThreshold int
	PollInterval   int
	IdleTimeout    int
}

var cfg Config

// ---- 全局实例 ----

var (
	globalTodo    *TodoManager
	globalSkills  *SkillLoader
	globalTaskMgr *TaskManager
	globalBg      *BackgroundManager
	globalBus     *MessageBus
	globalTeam    *TeammateManager

	globalSystemPrompt string

	// 会话管理
	sessions   = map[string]*Session{}
	sessionsMu sync.RWMutex
)

func getOrCreateSession(id string) *Session {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if s, ok := sessions[id]; ok {
		return s
	}
	s := &Session{ID: id, Input: []InputItem{}}
	sessions[id] = s
	slog.Info("new session created", "session_id", id)
	return s
}

// ---- 初始化 ----

func initConfig() {
	godotenv.Load()

	cfg = Config{
		BaseURL:        getEnv("OPENAI_BASE_URL", "https://api.openai.com"),
		APIKey:         getEnv("OPENAI_API_KEY", ""),
		ModelID:        getEnv("MODEL_ID", "gpt-4o"),
		WorkDir:        getEnvOrCwd("WORKDIR"),
		TokenThreshold: 100000,
		PollInterval:   5,
		IdleTimeout:    60,
	}

	slog.Info("config loaded",
		"base_url", cfg.BaseURL,
		"model_id", cfg.ModelID,
		"work_dir", cfg.WorkDir,
	)

	if cfg.APIKey == "" {
		slog.Warn("OPENAI_API_KEY is empty, LLM calls will fail")
	}
}

func initGlobals() {
	globalTodo = NewTodoManager()
	globalSkills = NewSkillLoader(filepath.Join(cfg.WorkDir, "skills"))
	globalTaskMgr = NewTaskManager(filepath.Join(cfg.WorkDir, ".tasks"))
	globalBg = NewBackgroundManager()
	globalBus = NewMessageBus(filepath.Join(cfg.WorkDir, ".team", "inbox"))
	globalTeam = NewTeammateManager(filepath.Join(cfg.WorkDir, ".team"), globalBus, globalTaskMgr)

	globalSystemPrompt = fmt.Sprintf(
		"You are a coding agent at %s. Use tools to solve tasks.\n"+
			"Prefer task_create/task_update/task_list for multi-step work. Use TodoWrite for short checklists.\n"+
			"Use task for subagent delegation. Use load_skill for specialized knowledge.\n"+
			"Skills: %s",
		cfg.WorkDir, globalSkills.Descriptions(),
	)

	slog.Info("globals initialized",
		"skills", globalSkills.Descriptions(),
		"system_prompt_len", len(globalSystemPrompt),
	)
}

// ---- HTTP 处理器 ----

type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// handleChat 处理聊天请求，返回 SSE 流
func handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if req.SessionID == "" {
		req.SessionID = "default"
	}
	if req.Message == "" {
		c.JSON(400, gin.H{"error": "message is required"})
		return
	}

	slog.Info("chat request", "session_id", req.SessionID, "message_len", len(req.Message))

	session := getOrCreateSession(req.SessionID)

	// 添加用户消息
	session.mu.Lock()
	session.Input = append(session.Input, InputItem{
		Type: "message",
		Role: "user",
		Content: []ContentPart{
			{Type: "input_text", Text: req.Message},
		},
	})
	session.mu.Unlock()

	// SSE 流
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// SSE 写入函数
	sseWriter := func(event SSEEvent) {
		data, _ := json.Marshal(event)
		c.SSEvent(event.Type, string(data))
		c.Writer.Flush()
	}

	// 运行 Agent 循环
	AgentLoop(session, sseWriter)

	// 发送完成事件
	sseWriter(SSEEvent{Type: "done", Data: map[string]string{}})
}

// handleTasks 查看任务列表
func handleTasks(c *gin.Context) {
	c.JSON(200, gin.H{"tasks": globalTaskMgr.ListAll()})
}

// handleTeam 查看队友状态
func handleTeam(c *gin.Context) {
	c.JSON(200, gin.H{"team": globalTeam.ListAll()})
}

// handleInbox 查看收件箱
func handleInbox(c *gin.Context) {
	msgs := globalBus.ReadInbox("lead")
	c.JSON(200, gin.H{"inbox": msgs})
}

// handleCompact 手动压缩
func handleCompact(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	c.ShouldBindJSON(&req)
	if req.SessionID == "" {
		req.SessionID = "default"
	}

	session := getOrCreateSession(req.SessionID)
	session.mu.Lock()
	session.Input = AutoCompact(session.Input)
	session.mu.Unlock()

	slog.Info("manual compact via API", "session_id", req.SessionID)
	c.JSON(200, gin.H{"status": "compacted"})
}

// ---- 启动入口 ----

func main() {
	// 配置 slog 输出到 stderr，详细级别
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("=== Go Agent starting ===")

	initConfig()
	initGlobals()

	r := gin.Default()

	// CORS 中间件
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 路由
	r.POST("/chat", handleChat)
	r.GET("/tasks", handleTasks)
	r.GET("/team", handleTeam)
	r.GET("/inbox", handleInbox)
	r.POST("/compact", handleCompact)

	port := getEnv("PORT", "8080")
	slog.Info("server starting", "port", port)
	if err := r.Run(":" + port); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// ---- 工具函数 ----

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvOrCwd(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	dir, _ := os.Getwd()
	return dir
}
