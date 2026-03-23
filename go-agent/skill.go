package main

// =====================================================
// skill.go - 技能加载器 (对应 Python 的 s05)
// 从 skills/ 目录扫描 SKILL.md 文件
// =====================================================

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Body        string
}

type SkillLoader struct {
	skills map[string]*Skill
}

func NewSkillLoader(dir string) *SkillLoader {
	sl := &SkillLoader{skills: map[string]*Skill{}}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		slog.Info("skills directory not found, skipping", "dir", dir)
		return sl
	}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "SKILL.md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("failed to read skill", "path", path, "error", err)
			return nil
		}

		text := string(data)
		name := filepath.Base(filepath.Dir(path))
		desc := ""
		body := text

		// 解析 YAML front matter: ---\nkey: value\n---
		if strings.HasPrefix(text, "---\n") {
			parts := strings.SplitN(text[4:], "\n---\n", 2)
			if len(parts) == 2 {
				// 解析 meta
				for _, line := range strings.Split(parts[0], "\n") {
					if idx := strings.Index(line, ":"); idx > 0 {
						k := strings.TrimSpace(line[:idx])
						v := strings.TrimSpace(line[idx+1:])
						switch k {
						case "name":
							name = v
						case "description":
							desc = v
						}
					}
				}
				body = strings.TrimSpace(parts[1])
			}
		}

		sl.skills[name] = &Skill{Name: name, Description: desc, Body: body}
		slog.Info("skill loaded", "name", name, "description", desc)
		return nil
	})

	return sl
}

// Descriptions 返回所有技能的描述列表
func (sl *SkillLoader) Descriptions() string {
	if len(sl.skills) == 0 {
		return "(no skills)"
	}
	var lines []string
	for name, s := range sl.skills {
		desc := s.Description
		if desc == "" {
			desc = "-"
		}
		lines = append(lines, fmt.Sprintf("  - %s: %s", name, desc))
	}
	return strings.Join(lines, "\n")
}

// Load 加载指定技能的内容
func (sl *SkillLoader) Load(name string) string {
	s, ok := sl.skills[name]
	if !ok {
		names := make([]string, 0, len(sl.skills))
		for n := range sl.skills {
			names = append(names, n)
		}
		return fmt.Sprintf("Error: Unknown skill '%s'. Available: %s", name, strings.Join(names, ", "))
	}
	slog.Info("skill loaded", "name", name)
	return fmt.Sprintf("<skill name=\"%s\">\n%s\n</skill>", name, s.Body)
}
