package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"axon/internal/graph"
)

// --- set_scope / clear_scope: session-level namespace restriction ---

type setScopeArgs struct {
	Projects []string `json:"projects"`
}

func (h *toolHandler) setScope(raw json.RawMessage) (*toolResult, error) {
	var a setScopeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if len(a.Projects) == 0 {
		return nil, fmt.Errorf("projects 不能为空")
	}
	h.activeScope = a.Projects
	return textResult(fmt.Sprintf("已锁定范围到 %v。后续 search/remember/get_entity 只在这些命名空间内操作。用 clear_scope 恢复全量。", a.Projects)), nil
}

func (h *toolHandler) clearScope() (*toolResult, error) {
	h.activeScope = nil
	return textResult("已清除范围锁定，恢复为搜索全部命名空间。"), nil
}

// --- move_entity: move an entity from one namespace to another ---

type moveEntityArgs struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

func (h *toolHandler) moveEntity(raw json.RawMessage) (*toolResult, error) {
	var a moveEntityArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	a.Name = strings.TrimSpace(a.Name)
	a.From = strings.TrimSpace(a.From)
	a.To = strings.TrimSpace(a.To)
	if a.Name == "" || a.From == "" || a.To == "" {
		return nil, fmt.Errorf("name, from, to 都必填")
	}
	if a.From == a.To {
		return textResult("源和目标相同，无需移动。"), nil
	}

	// 1. Find and extract the entity from source namespace.
	fromDir := filepath.Join(h.dataDir, "graphcache", a.From)
	entries, err := os.ReadDir(fromDir)
	if err != nil {
		return textResult(fmt.Sprintf("源命名空间 `%s` 不存在。", a.From)), nil
	}

	target := strings.ToLower(a.Name)
	var foundEntity *graph.Entity
	var foundRelations []graph.Relation
	removedFrom := 0

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(fromDir, entry.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		var cache graph.SessionCache
		if json.Unmarshal(data, &cache) != nil {
			continue
		}

		origLen := len(cache.Entities)
		var kept []graph.Entity
		for i := range cache.Entities {
			if matchesEntity(cache.Entities[i], target) {
				if foundEntity == nil {
					e := cache.Entities[i]
					foundEntity = &e
				}
				continue
			}
			kept = append(kept, cache.Entities[i])
		}

		origRels := len(cache.Relations)
		var keptRels []graph.Relation
		for _, r := range cache.Relations {
			if strings.ToLower(r.From) == target || strings.ToLower(r.To) == target {
				foundRelations = append(foundRelations, r)
				continue
			}
			keptRels = append(keptRels, r)
		}

		if len(kept) == origLen && len(keptRels) == origRels {
			continue
		}

		cache.Entities = kept
		cache.Relations = keptRels
		removedFrom++

		if len(cache.Entities) == 0 && len(cache.Relations) == 0 && len(cache.Chunks) == 0 {
			os.Remove(fpath)
		} else {
			out, _ := json.MarshalIndent(cache, "", "  ")
			_ = os.WriteFile(fpath, out, 0o644)
		}
	}

	if foundEntity == nil {
		return textResult(fmt.Sprintf("在命名空间 `%s` 中没有找到实体「%s」。", a.From, a.Name)), nil
	}

	// 2. Write the entity into target namespace.
	toDir := filepath.Join(h.dataDir, "graphcache", a.To)
	if err := os.MkdirAll(toDir, 0o755); err != nil {
		return nil, fmt.Errorf("create target dir: %w", err)
	}

	source := fmt.Sprintf("moved:%d", time.Now().UnixMilli())
	moveCache := &graph.SessionCache{
		SessionID: source,
		Mtime:     time.Now().UnixMilli(),
		Schema:    graph.CacheSchema,
		Entities:  []graph.Entity{*foundEntity},
		Relations: foundRelations,
	}
	if err := graph.SaveCache(h.dataDir, a.To, moveCache); err != nil {
		return nil, fmt.Errorf("save to target: %w", err)
	}

	return textResult(fmt.Sprintf("已将实体「%s」从 `%s` 移动到 `%s`（含 %d 条关系）。", a.Name, a.From, a.To, len(foundRelations))), nil
}
