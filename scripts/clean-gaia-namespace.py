#!/usr/bin/env python3
"""Clean gaia namespace: remove noise, extract platform infra to _global_."""
import json
import os
import glob
import shutil

DATA_DIR = os.path.expanduser("~/Library/Application Support/axon")
GAIA_GC = os.path.join(DATA_DIR, "graphcache", "gaia")
GLOBAL_GC = os.path.join(DATA_DIR, "graphcache", "_global_")

# --- Define what to remove (noise) ---
NOISE_NAMES = {
    "产检与个体化营养指导", "加工肉制品", "叶酸", "孕中期", "孕早期", "孕晚期",
    "孕期咖啡因限制", "孕期烧烤饮食", "孕期特殊情况饮食管理", "孕期酒精禁忌",
    "孕期食品安全限制", "孕期饮食卫生", "孕期饮食管理", "烧烤饮食搭配",
    "生食及生腌海鲜", "铁", "食物彻底加热", "高盐高辣饮食",
    # Axon-related misplaced
    "Obsidian", "Obsidian Skills", "codegraph 代码探索工具", "codegraph索引",
    "obsidian-bases", "obsidian-cli", "obsidian-markdown",
}

# --- Define what to move to _global_ (platform infra) ---
GLOBAL_NAMES = {
    "GitLab CI", "Hippo", "Hippo 配置中心", "Hippo 镜像 ancestry 检查",
    "Hippo 镜像版本防回退校验", "Hippo分支级发布约束", "Hippo配置中心",
    "Kubernetes API SSL证书问题", "公共 Kubernetes 集群", "基建-自建K8s",
    "自建私有 Kubernetes 集群", "镜像发布约束", "镜像部署约束",
}
# Note: specific pipeline IDs (CI Pipeline 204441 etc.) and env-specific
# entities (sit-14, dev-66 etc.) stay in gaia — they're deployment context
# for the business project, not reusable platform knowledge.

os.makedirs(GLOBAL_GC, exist_ok=True)

removed_count = 0
moved_count = 0
global_entities = []
global_relations = []

for fpath in sorted(glob.glob(os.path.join(GAIA_GC, "*.json"))):
    with open(fpath) as fh:
        try:
            cache = json.load(fh)
        except:
            continue

    entities = cache.get("entities") or []
    relations = cache.get("relations") or []
    original_len = len(entities)

    # Separate: keep / remove / move-to-global
    keep_ents = []
    for ent in entities:
        name = ent.get("name", "")
        if name in NOISE_NAMES:
            removed_count += 1
        elif name in GLOBAL_NAMES:
            global_entities.append(ent)
            moved_count += 1
        else:
            keep_ents.append(ent)

    # Filter relations: remove those touching noise entities
    all_removed = NOISE_NAMES | GLOBAL_NAMES
    keep_rels = []
    global_rels_local = []
    for rel in relations:
        from_name = rel.get("from", "")
        to_name = rel.get("to", "")
        if from_name in NOISE_NAMES or to_name in NOISE_NAMES:
            continue  # drop
        elif from_name in GLOBAL_NAMES or to_name in GLOBAL_NAMES:
            global_rels_local.append(rel)
        else:
            keep_rels.append(rel)
    global_relations.extend(global_rels_local)

    if len(keep_ents) != original_len or len(keep_rels) != len(relations):
        cache["entities"] = keep_ents
        cache["relations"] = keep_rels
        with open(fpath, "w") as fh:
            json.dump(cache, fh, ensure_ascii=False, indent=2)

# Write global entities as a single cache entry
if global_entities:
    global_cache = {
        "sessionId": "migrated-infra",
        "mtime": 0,
        "schema": 2,
        "entities": global_entities,
        "relations": global_relations,
    }
    out_path = os.path.join(GLOBAL_GC, "migrated-infra.json")
    with open(out_path, "w") as fh:
        json.dump(global_cache, fh, ensure_ascii=False, indent=2)

print(f"Removed {removed_count} noise entities")
print(f"Moved {moved_count} entities to _global_")
print(f"Global relations: {len(global_relations)}")
