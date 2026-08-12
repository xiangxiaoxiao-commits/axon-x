package graph

// Merge folds newly extracted entities/relations into g, de-duplicating by
// normalized name so re-processing accumulates knowledge instead of piling up
// duplicate nodes. Observations (with their sources) are unioned; relations are
// de-duped by triple.
//
// Alias normalization: an entity is matched to an existing one when its name OR
// any of its aliases collides with the existing entity's name OR any of its
// aliases. This collapses "支付服务"/"PaymentService"/"payment" into a single
// node instead of three fragments. A "name/alias -> entity index" map drives the
// lookup; every alias a merged entity carries is registered so later entities
// find it too.
func (g *Graph) Merge(entities []Entity, relations []Relation) {
	// Index every existing entity by its name and each of its aliases, so a new
	// entity matching any of those keys folds into it.
	idx := make(map[string]int, len(g.Entities))
	for i := range g.Entities {
		for _, k := range entityKeys(g.Entities[i]) {
			idx[k] = i
		}
	}

	for _, ne := range entities {
		key := normKey(ne.Name)
		if key == "" {
			continue
		}
		// Find an existing entity sharing the name or any alias.
		target := -1
		for _, k := range entityKeys(ne) {
			if i, ok := idx[k]; ok {
				target = i
				break
			}
		}
		if target >= 0 {
			e := &g.Entities[target]
			// Union observations and their sources in lockstep.
			e.Observations, e.ObsSources = unionObs(
				e.Observations, e.ObsSources, ne.Observations, ne.ObsSources)
			if e.Type == "" {
				e.Type = ne.Type
			}
			// Fold the new entity's name and aliases into the canonical node so it
			// stays discoverable under all its names.
			e.Aliases = mergeAliases(e.Name, e.Aliases, ne)
			// Embedding: latest non-empty wins, so the freshest vector (from the
			// most recently indexed session) is kept.
			if len(ne.Embedding) > 0 {
				e.Embedding = ne.Embedding
			}
			// Register any newly-added keys so future entities collide with them.
			for _, k := range entityKeys(*e) {
				idx[k] = target
			}
		} else {
			ne.Observations, ne.ObsSources = unionObs(nil, nil, ne.Observations, ne.ObsSources)
			ne.Aliases = mergeAliases(ne.Name, nil, ne)
			g.Entities = append(g.Entities, ne)
			pos := len(g.Entities) - 1
			for _, k := range entityKeys(g.Entities[pos]) {
				idx[k] = pos
			}
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

// entityKeys returns the normalized lookup keys for an entity: its name plus
// every alias, empties dropped.
func entityKeys(e Entity) []string {
	out := make([]string, 0, 1+len(e.Aliases))
	if k := normKey(e.Name); k != "" {
		out = append(out, k)
	}
	for _, a := range e.Aliases {
		if k := normKey(a); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// mergeAliases builds the alias set for a canonical entity named canonical: the
// existing aliases, plus the incoming entity's name and aliases, de-duplicated
// case-insensitively and with the canonical name itself excluded (it is not its
// own alias). Order is preserved.
func mergeAliases(canonical string, existing []string, ne Entity) []string {
	canon := normKey(canonical)
	seen := map[string]bool{canon: true}
	out := make([]string, 0, len(existing)+1+len(ne.Aliases))
	add := func(s string) {
		k := normKey(s)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	for _, a := range existing {
		add(a)
	}
	add(ne.Name)
	for _, a := range ne.Aliases {
		add(a)
	}
	return out
}

func relKey(r Relation) string {
	return normKey(r.From) + "\x00" + normKey(r.To) + "\x00" + normKey(r.Label)
}

// unionObs appends observations from addObs to baseObs, skipping empties and
// case-insensitive duplicates, keeping a parallel source slice aligned. The
// source of an added observation is addSrc[i] when present, otherwise "".
// baseSrc is normalized to the same length as baseObs first (older caches may
// carry no sources). Order is preserved.
func unionObs(baseObs, baseSrc, addObs, addSrc []string) (obs, src []string) {
	seen := make(map[string]bool, len(baseObs)+len(addObs))
	obs = make([]string, 0, len(baseObs)+len(addObs))
	src = make([]string, 0, len(baseObs)+len(addObs))
	emit := func(o, s string) {
		k := normKey(o)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		obs = append(obs, o)
		src = append(src, s)
	}
	for i, o := range baseObs {
		emit(o, at(baseSrc, i))
	}
	for i, o := range addObs {
		emit(o, at(addSrc, i))
	}
	return obs, src
}

// at returns s[i] or "" when i is out of range (parallel-array safety for older
// caches whose source slice is shorter than or missing next to observations).
func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}
