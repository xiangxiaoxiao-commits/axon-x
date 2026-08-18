package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"axon/internal/graph"
)

// deleteEntityArgs is the delete_entity argument shape.
type deleteEntityArgs struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// deleteEntity removes an entity (and its relations) from all cache files in a
// project's graphcache directory. Matches by name or alias, case-insensitive.
func (h *toolHandler) deleteEntity(raw json.RawMessage) (*toolResult, error) {
	var a deleteEntityArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return nil, fmt.Errorf("name 必填")
	}
	slug, _ := h.resolveSlug(a.Project)
	if slug == "" {
		return nil, fmt.Errorf("无法确定项目：当前目录没有 .axon-project 文件，请显式传 project 参数或创建 .axon-project")
	}

	dir := filepath.Join(h.dataDir, "graphcache", slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return textResult(fmt.Sprintf("命名空间 `%s` 没有 graphcache 目录。", slug)), nil
	}

	target := strings.ToLower(a.Name)
	removedFrom := 0

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		var cache graph.SessionCache
		if json.Unmarshal(data, &cache) != nil {
			continue
		}

		// Find and remove matching entities.
		origLen := len(cache.Entities)
		var kept []graph.Entity
		for _, e := range cache.Entities {
			if matchesEntity(e, target) {
				continue // remove
			}
			kept = append(kept, e)
		}

		// Remove relations touching the entity.
		origRels := len(cache.Relations)
		var keptRels []graph.Relation
		for _, r := range cache.Relations {
			if strings.ToLower(r.From) == target || strings.ToLower(r.To) == target {
				continue
			}
			keptRels = append(keptRels, r)
		}

		if len(kept) == origLen && len(keptRels) == origRels {
			continue // nothing changed in this file
		}

		cache.Entities = kept
		cache.Relations = keptRels
		removedFrom++

		// Write back (or delete file if empty).
		if len(cache.Entities) == 0 && len(cache.Relations) == 0 && len(cache.Chunks) == 0 {
			os.Remove(fpath)
		} else {
			out, _ := json.MarshalIndent(cache, "", "  ")
			_ = os.WriteFile(fpath, out, 0o644)
		}
	}

	if removedFrom == 0 {
		return textResult(fmt.Sprintf("在命名空间 `%s` 中没有找到实体「%s」。", slug, a.Name)), nil
	}
	return textResult(fmt.Sprintf("已从命名空间 `%s` 的 %d 个缓存文件中删除实体「%s」及其关系。", slug, removedFrom, a.Name)), nil
}

// matchesEntity checks if an entity matches the target (by name or alias,
// case-insensitive).
func matchesEntity(e graph.Entity, target string) bool {
	if strings.ToLower(strings.TrimSpace(e.Name)) == target {
		return true
	}
	for _, al := range e.Aliases {
		if strings.ToLower(strings.TrimSpace(al)) == target {
			return true
		}
	}
	return false
}
