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

	"go-agent/tools"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	toolRegistry       *tools.Registry

	// 关机/计划审批状态
	shutdownRequests = map[string]map[string]string{}
	planRequests     = map[string]map[string]string{}

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
	slog.Info("config loaded", "base_url", cfg.BaseURL, "model_id", cfg.ModelID, "work_dir", cfg.WorkDir)
	if cfg.APIKey == "" {
		slog.Warn("OPENAI_API_KEY is empty")
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
			"Use task for subagent delegation. Use load_skill for specialized knowledge.\nSkills: %s",
		cfg.WorkDir, globalSkills.Descriptions(),
	)

	// 初始化工具注册表（把主包的管理器通过回调注入 tools 包）
	toolRegistry = &tools.Registry{
		WorkDir: cfg.WorkDir,

		TodoUpdate: func(items json.RawMessage) string {
			var todoItems []TodoItem
			if err := json.Unmarshal(items, &todoItems); err != nil {
				return fmt.Sprintf("Error: %v", err)
			}
			result, err := globalTodo.Update(todoItems)
			if err != nil {
				return fmt.Sprintf("Error: %v", err)
			}
			return result
		},
		RunSubagent:   runSubagent, // 定义在 agent.go
		SkillLoad:     func(name string) string { return globalSkills.Load(name) },
		BgRun:         func(cmd string, timeout int) string { return globalBg.Run(cmd, timeout) },
		BgCheck:       func(taskID string) string { return globalBg.Check(taskID) },
		TaskCreate:    func(subj, desc string) string { return globalTaskMgr.Create(subj, desc) },
		TaskGet:       func(id int) string { return globalTaskMgr.Get(id) },
		TaskUpdate:    func(id int, s string, bb, bl []int) string { return globalTaskMgr.Update(id, s, bb, bl) },
		TaskList:      func() string { return globalTaskMgr.ListAll() },
		TaskClaim:     func(id int, owner string) string { return globalTaskMgr.Claim(id, owner) },
		SpawnTeammate: func(n, r, p string) string { return globalTeam.Spawn(n, r, p) },
		ListTeammates: func() string { return globalTeam.ListAll() },
		SendMessage: func(to, content, msgType string) string {
			return globalBus.Send("lead", to, content, msgType, nil)
		},
		ReadInbox: func() string {
			msgs := globalBus.ReadInbox("lead")
			data, _ := json.MarshalIndent(msgs, "", "  ")
			return string(data)
		},
		Broadcast: func(content string) string {
			return globalBus.Broadcast("lead", content, globalTeam.MemberNames())
		},
		ShutdownRequest: func(teammate string) string {
			reqID := uuid.New().String()[:8]
			shutdownRequests[reqID] = map[string]string{"target": teammate, "status": "pending"}
			globalBus.Send("lead", teammate, "Please shut down.", "shutdown_request", map[string]any{"request_id": reqID})
			slog.Info("shutdown requested", "request_id", reqID, "teammate", teammate)
			return fmt.Sprintf("Shutdown request %s sent to '%s'", reqID, teammate)
		},
		PlanApproval: func(requestID string, approve bool, feedback string) string {
			req, ok := planRequests[requestID]
			if !ok {
				return fmt.Sprintf("Error: Unknown plan request_id '%s'", requestID)
			}
			status := "rejected"
			if approve {
				status = "approved"
			}
			req["status"] = status
			globalBus.Send("lead", req["from"], feedback, "plan_approval_response",
				map[string]any{"request_id": requestID, "approve": approve, "feedback": feedback})
			return fmt.Sprintf("Plan %s for '%s'", status, req["from"])
		},
	}

	slog.Info("globals initialized", "system_prompt_len", len(globalSystemPrompt))
}

// ---- HTTP 处理器 ----

type ChatReq struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

func handleChat(c *gin.Context) {
	var req ChatReq
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

	session.mu.Lock()
	session.Input = append(session.Input, InputItem{
		Type: "message", Role: "user",
		Content: []ContentPart{{Type: "input_text", Text: req.Message}},
	})
	session.mu.Unlock()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	sseWriter := func(event SSEEvent) {
		data, _ := json.Marshal(event)
		c.SSEvent(event.Type, string(data))
		c.Writer.Flush()
	}

	AgentLoop(session, sseWriter)
	sseWriter(SSEEvent{Type: "done", Data: map[string]string{}})
}

func handleTasks(c *gin.Context)   { c.JSON(200, gin.H{"tasks": globalTaskMgr.ListAll()}) }
func handleTeam(c *gin.Context)    { c.JSON(200, gin.H{"team": globalTeam.ListAll()}) }
func handleInbox(c *gin.Context)   { c.JSON(200, gin.H{"inbox": globalBus.ReadInbox("lead")}) }

func handleCompact(c *gin.Context) {
	var req struct{ SessionID string `json:"session_id"` }
	c.ShouldBindJSON(&req)
	if req.SessionID == "" {
		req.SessionID = "default"
	}
	session := getOrCreateSession(req.SessionID)
	session.mu.Lock()
	session.Input = AutoCompact(session.Input)
	session.mu.Unlock()
	c.JSON(200, gin.H{"status": "compacted"})
}

// ---- 启动入口 ----

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("=== Go Agent starting ===")

	initConfig()
	initGlobals()

	r := gin.Default()
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
