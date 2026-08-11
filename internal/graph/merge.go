package graph

// Merge folds newly extracted entities/relations into g, de-duplicating by
// normalized name so re-processing accumulates knowledge instead of piling up
// duplicate nodes. Observations are unioned; relations are de-duped by triple.
func (g *Graph) Merge(entities []Entity, relations []Relation) {
	// Index existing entities by normalized name.
	idx := make(map[string]int, len(g.Entities))
	for i, e := range g.Entities {
		idx[normKey(e.Name)] = i
	}

	for _, ne := range entities {
		key := normKey(ne.Name)
		if key == "" {
			continue
		}
		if i, ok := idx[key]; ok {
			// Existing entity: union observations, keep first non-empty type.
			g.Entities[i].Observations = unionStrings(g.Entities[i].Observations, ne.Observations)
			if g.Entities[i].Type == "" {
				g.Entities[i].Type = ne.Type
			}
			// Embedding: latest non-empty wins, so the freshest vector (from the
			// most recently indexed session) is kept.
			if len(ne.Embedding) > 0 {
				g.Entities[i].Embedding = ne.Embedding
			}
		} else {
			ne.Observations = unionStrings(nil, ne.Observations)
			g.Entities = append(g.Entities, ne)
			idx[key] = len(g.Entities) - 1
		}
	}

	// De-dupe relations by (from,to,label), normalized.
	seen := make(map[string]bool, len(g.Relations))
	for _, r := range g.Relations {
		seen[relKey(r)] = true
	}
	for _, nr := range relations {
		if normKey(nr.From) == "" || normKey(nr.To) == "" {
			continue
		}
		k := relKey(nr)
		if seen[k] {
			continue
		}
		seen[k] = true
		g.Relations = append(g.Relations, nr)
	}
}

func relKey(r Relation) string {
	return normKey(r.From) + "\x00" + normKey(r.To) + "\x00" + normKey(r.Label)
}

// unionStrings appends items from add to base, skipping empties and
// case-insensitive duplicates. Order is preserved.
func unionStrings(base, add []string) []string {
	seen := make(map[string]bool, len(base)+len(add))
	out := make([]string, 0, len(base)+len(add))
	for _, s := range append(append([]string{}, base...), add...) {
		k := normKey(s)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}
