# 功能：会话溯源 + 逐条排除知识

## 目标
在会话栏里看到「本会话被总结进知识图谱的哪些知识」，并能删除某条 / 阻止它再被总结进去（删除要扛得过重新建索引）。

## 现状（已确认）
- `Entity.ObsSources[i]` = `Observations[i]` 的来源会话 id，溯源信息已存在。
- 每个会话的产物就是缓存文件 `graphcache/<slug>/<sessionID>.json`（`SessionCache`：entities/relations/chunks）。**不用反查，直接读这个文件就是「本会话产出」。**
- `assembleGraph` 每次从所有缓存重建 `graph.json`。所以只删最终图 → 下次重建被缓存覆盖。**这是必须解决的核心。**
- `LoadAllCache` 读缓存目录下所有 `.json` → 排除清单不能放这个目录、也不能用 `.json` 后缀。
- Relations 无来源字段（本期不追关系溯源，只做 observation 级）。

## 方案：排除清单（exclusion list），在两处生效
排除清单存 `graphcache/<slug>/.exclusions.json`（点前缀 + 已被 `LoadAllCache` 的 `.json` 判断放行？不——它匹配 `.json` 后缀，所以改存 `graphcache/<slug>.exclusions`（放在 slug 目录**外**，无 `.json` 后缀），避免被当缓存解析）。

内容：`{ "obs": ["<sha1(entityName|obsText)>", ...] }` —— 用实体名+事实文本的哈希做稳定 key，不依赖易变的下标。

生效两处：
1. **assembleGraph 合并后过滤**：merge 完，按排除清单剔掉命中的 observation（连带 ObsSources）；实体的 observation 全被剔则删实体。→ 删除立即在图上生效，且**每次重建都重新过滤，不会被带回来**。
2. **IndexProject 蒸馏后过滤**（可选加固）：新会话蒸馏出的 entities 落缓存前也过一遍，省得写进缓存再被过滤。

## 后端改动
- `internal/graph`：新增 `LoadExclusions/SaveExclusions`，`obsKey(entity,obs)` 哈希，`FilterExcluded(g, set)`。
- `graphbuild.go::assembleGraph`：merge 后调用 `FilterExcluded`。
- 新增 Wails 方法：
  - `SessionDistilledKnowledge(slug, sessionID) -> {entities:[{name,observations:[{text,excluded}]}], relations}`：读该会话缓存 + 标注哪些已被排除。
  - `ExcludeObservation(slug, entityName, obsText)` / `UnexcludeObservation(...)`：改排除清单 + 重新 assemble。

## 前端改动（SessionsView）
- 选中会话 → 详情页新增「本会话产出的知识」面板：列出该会话缓存里的实体+事实。
- 每条事实一个「✕ 不要这条」按钮 → 调 `ExcludeObservation`，标灰/划掉；可撤销。
- 复用 GraphView 现有的删除/编辑交互风格。

## 边界
- 只做 observation 级排除（关系无来源，本期不做）。
- 排除是「按内容哈希」：同一条事实若多个会话都产出，排除会全局生效（符合直觉：这条知识我不想要）。
- 不删缓存原文件（保留可追溯 + 可撤销）。
