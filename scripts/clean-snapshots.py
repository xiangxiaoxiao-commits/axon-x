#!/usr/bin/env python3
"""Clean snapshot-type observations and entities from the axon knowledge graph.

Snapshots are one-time states (specific pipeline IDs, image tags, commit hashes,
IP addresses, pod statuses) that become stale immediately. Rules are durable
knowledge (workflow patterns, naming conventions, architecture decisions).

This script:
1. Removes entirely entities whose ALL observations are snapshots.
2. Trims snapshot observations from mixed entities (keeps rules only).
3. Removes relations touching deleted entities.
4. Rewrites cache files in place.
"""

import json
import os
import re
import glob
import platform
import sys


def resolve_data_dir():
    """Resolve the axon data directory (cross-platform)."""
    if sys.platform == "win32":
        base = os.environ.get("APPDATA", os.path.expanduser("~\\AppData\\Roaming"))
    elif sys.platform == "darwin":
        base = os.path.expanduser("~/Library/Application Support")
    else:
        base = os.environ.get("XDG_CONFIG_HOME", os.path.expanduser("~/.config"))
    return os.path.join(base, "axon")


DATA_DIR = resolve_data_dir()
GC_DIR = os.path.join(DATA_DIR, "graphcache")

# --- Snapshot detection patterns ---

# Image tags: x.y.z-beta-<hex> or x.y.z-stable-<hex>
RE_IMAGE_TAG = re.compile(r"\d+\.\d+\.\d+-(?:beta|stable)-[0-9a-f]{6,}")
# IP addresses
RE_IP = re.compile(r"(?:10\.12\.0\.78|10\.99\.\d+|172\.20\.\d+|39\.105\.\d+)")
# Pipeline IDs: "pipeline 20xxxx" or "pipeline#20xxxx"
RE_PIPELINE = re.compile(r"pipeline\s*#?\s*\d{5,}")
# Commit hashes: 7+ hex chars that look like git SHAs (word boundary)
RE_COMMIT = re.compile(r"\b[0-9a-f]{7,40}\b")
# Specific image repo
RE_IMAGE_REPO = re.compile(r"10\.12\.0\.78:5000/")

SNAPSHOT_KEYWORDS = [
    "单副本 Running",
    "Pod 状态为 Running",
    "Pod 状态为",
    "已扩容",
    "已推送到 release/",
    "已合入 release/",
    "已 push 到",
    "已推送远程",
    "已 push",
    "提交为 ",
    "提交 commit",
    "最终提交为",
    "修复提交为",
    "Deployment gaia-lite",
    "容器名为 ccc",
    "当前镜像为",
    "修复镜像为",
    "构建镜像为",
    "部署镜像",
    "镜像为 ",
    "pipeline 均成功",
    "构建成功",
    "CI 成功",
    "pipeline 构建成功",
]

# Rule indicators: if an observation matches these, it's durable knowledge
RULE_INDICATORS = [
    "push 后自动触发",
    "push 后会自动",
    "自动触发构建",
    "release 分支",
    "收发双方运行同一套",
    "ClusterIP 第三段",
    "namespace 对应",
    "API类型增加",
    "接口",
    "约定",
    "约束",
    "必须",
    "不能",
    "不允许",
    "规则",
    "规范",
    "流程",
    "架构",
    "职责",
    "负责",
    "所在服务",
    "设计决策",
    "原因是",
    "因为",
    "为了",
]


def is_snapshot(obs: str) -> bool:
    """Classify one observation as snapshot (True) or rule (False)."""
    # First check if it's explicitly a rule
    for indicator in RULE_INDICATORS:
        if indicator in obs:
            return False

    # Check snapshot patterns
    if RE_IMAGE_TAG.search(obs):
        return True
    if RE_IP.search(obs):
        return True
    if RE_PIPELINE.search(obs):
        return True
    if RE_IMAGE_REPO.search(obs):
        return True

    # Commit hash check: need at least one keyword context to avoid false positives
    commit_contexts = ["提交", "commit", "push", "合入", "cherry-pick", "rebase"]
    if RE_COMMIT.search(obs) and any(ctx in obs.lower() for ctx in commit_contexts):
        return True

    for kw in SNAPSHOT_KEYWORDS:
        if kw in obs:
            return True

    return False


def main():
    namespaces = [
        d for d in os.listdir(GC_DIR)
        if os.path.isdir(os.path.join(GC_DIR, d))
    ]

    total_entities_deleted = 0
    total_entities_trimmed = 0
    total_obs_removed = 0
    total_files_modified = 0

    for ns in sorted(namespaces):
        gc_path = os.path.join(GC_DIR, ns)
        for fpath in sorted(glob.glob(os.path.join(gc_path, "*.json"))):
            with open(fpath) as fh:
                try:
                    cache = json.load(fh)
                except (json.JSONDecodeError, ValueError):
                    continue

            entities = cache.get("entities") or []
            relations = cache.get("relations") or []
            if not entities:
                continue

            orig_ent_count = len(entities)
            orig_rel_count = len(relations)

            kept_entities = []
            deleted_names = set()

            for ent in entities:
                name = ent.get("name", "")
                obs = ent.get("observations") or []
                obs_sources = ent.get("obsSources") or []

                if not obs:
                    kept_entities.append(ent)
                    continue

                # Classify each observation
                kept_obs = []
                kept_sources = []
                removed_count = 0

                for i, o in enumerate(obs):
                    if is_snapshot(o):
                        removed_count += 1
                    else:
                        kept_obs.append(o)
                        if i < len(obs_sources):
                            kept_sources.append(obs_sources[i])

                if removed_count == 0:
                    # All observations are rules, keep as-is
                    kept_entities.append(ent)
                elif len(kept_obs) == 0:
                    # All observations are snapshots, delete entity
                    deleted_names.add(name.lower())
                    total_entities_deleted += 1
                    total_obs_removed += removed_count
                else:
                    # Mixed: keep only rule observations
                    ent["observations"] = kept_obs
                    ent["obsSources"] = kept_sources
                    kept_entities.append(ent)
                    total_entities_trimmed += 1
                    total_obs_removed += removed_count

            # Remove relations touching deleted entities
            if deleted_names:
                kept_relations = [
                    r for r in relations
                    if r.get("from", "").lower() not in deleted_names
                    and r.get("to", "").lower() not in deleted_names
                ]
            else:
                kept_relations = relations

            # Write back if anything changed
            if len(kept_entities) != orig_ent_count or len(kept_relations) != orig_rel_count:
                cache["entities"] = kept_entities
                cache["relations"] = kept_relations
                total_files_modified += 1

                if not kept_entities and not kept_relations and not cache.get("chunks"):
                    os.remove(fpath)
                else:
                    with open(fpath, "w") as fh:
                        json.dump(cache, fh, ensure_ascii=False, indent=2)

    print("=== Knowledge Graph Snapshot Cleanup ===")
    print(f"Entities deleted entirely: {total_entities_deleted}")
    print(f"Entities trimmed (snapshot obs removed): {total_entities_trimmed}")
    print(f"Total observations removed: {total_obs_removed}")
    print(f"Cache files modified: {total_files_modified}")


if __name__ == "__main__":
    main()
