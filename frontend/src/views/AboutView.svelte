<script lang="ts">
  // "关于 Axon" overlay: a self-contained explainer of what the app does and
  // how it works, opened by clicking the sidebar brand. Pure static content —
  // no backend calls — so it always works, even before anything is configured.
  export let onClose: () => void = () => {};

  // Close on Escape for keyboard users.
  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }
</script>

<svelte:window on:keydown={onKey} />

<div class="overlay" role="button" tabindex="-1" aria-label="关闭" on:click={onClose} on:keydown={(e) => (e.key === "Enter" || e.key === " ") && onClose()}>
  <div class="panel" role="dialog" aria-modal="true" aria-label="关于 Axon" tabindex="-1" on:click|stopPropagation>
    <header class="head">
      <div class="title">axon · 功能与原理</div>
      <button class="close" on:click={onClose} aria-label="关闭">✕</button>
    </header>

    <div class="body">
      <h3>这是什么</h3>
      <p class="lead">
        Axon 是给 AI 编码助手(尤其 Claude Code)用的<strong>上下文增强器</strong>。它把你项目里
        散落的"隐性知识"——历史对话里的设计决策、代码结构、踩过的坑、接口约定——沉淀成一张
        <strong>知识图谱</strong>,再通过 MCP 暴露给 agent。这样 AI 每次干活前都能先"读懂你的项目",
        而不是每次从零开始、要你反复解释。
      </p>
      <p class="lead">一句话:<strong>用得越多越懂你的项目。</strong></p>

      <hr class="divider" />

      <h3>核心闭环:建图 → 检索 → 注入</h3>
      <div class="step"><span class="num">1</span><span><strong>建图(Ingest)</strong>:从三类来源抽取"实体 + 关系 + 原文片段",合并进按项目隔离的图谱——① 代码仓库(目录/文件/包/import,Go 还抽函数与调用);② Claude Code 历史对话;③ Obsidian 笔记。</span></div>
      <div class="step"><span class="num">2</span><span><strong>检索(Recall)</strong>:给一句话,双通道并行召回——"实体结构"通道(找到相关实体,再沿关系扩展一跳)+ "原文片段"通道,用 RRF 融合排序。</span></div>
      <div class="step"><span class="num">3</span><span><strong>注入(Inject)</strong>:通过 MCP 把召回结果交给 agent,带<strong>来源溯源</strong>(这条知识来自哪次对话/哪个文件)。</span></div>

      <hr class="divider" />

      <h3>界面:两个入口</h3>
      <p><span class="tag">⊹ 知识</span>查看和养这张图谱。</p>
      <ul>
        <li><strong>建索引</strong>:读这个项目的 Claude Code 会话,提炼知识入图。</li>
        <li><strong>🧬 从代码建图</strong>:扫一个仓库,抽出模块/文件/函数及依赖关系。</li>
        <li><strong>📓 吸收 Obsidian</strong>:把 vault 笔记并入图谱。</li>
        <li><strong>可视化 + 人工校正</strong>:看实体/关系/别名/溯源,可确认、修正、删除去噪——<strong>图谱质量比数量重要</strong>。</li>
        <li><strong>📖 阅读文章</strong>:把图谱里的知识生成一篇可读的项目说明。</li>
      </ul>
      <p><span class="tag">⚙ 设置</span>配置模型与接入。</p>
      <ul>
        <li><strong>Providers</strong>:加模型服务(OpenAI / Anthropic / 智谱 GLM / 自定义网关),key 只存系统凭证库,不落明文。</li>
        <li><strong>召回方式</strong>:关键词 or 语义模型二选一(见下)。</li>
        <li><strong>接入 Claude Code</strong>:一键把图谱接给 agent(见下)。</li>
      </ul>

      <hr class="divider" />

      <h3>召回方式:关键词 vs 语义模型</h3>
      <p>这是一个<strong>显式开关,二选一,不做悄悄降级</strong>:</p>
      <ul>
        <li><span class="tag">🔤 关键词</span><strong>本地词面向量</strong>(纯 Go、离线、零成本)。按字面 / n-gram 近似召回,中文友好,不调用云端。</li>
        <li><span class="tag">🧠 语义模型</span><strong>只用</strong>你配置的云端 embedding 模型,按"意思"召回,精度高。<strong>失败不降级</strong>——云端挂了就直接报错、召回退回关键词并明确提示,而不是偷偷用本地假装成语义。</li>
      </ul>

      <h3>什么时候会调用云端语义模型</h3>
      <p>仅当开关设为「语义模型」时,发生在两个时刻:</p>
      <ul>
        <li><strong>写入(建向量)</strong>:建索引 / 从代码建图 / 吸收 Obsidian / 回写。内容多,是 token 消耗大头。</li>
        <li><strong>查询(召回)</strong>:阅读文章,以及 <strong>agent 通过 MCP 查询</strong>。只 embed 一句话,很便宜。</li>
      </ul>
      <p class="lead">
        ⚠️ <strong>切换模式后建议重新建图</strong>:存储向量和查询向量必须同源——关键词模式存的是本地向量,
        语义模式存的是云端向量,两者不通用。切换后重跑一次"建索引 / 从代码建图",新模式才完全生效。
      </p>

      <details class="deep">
        <summary>深入:语义召回链路的完整算法(点击展开)</summary>
        <div class="deep-body">

          <h4>① 文本 → 向量(embedding)</h4>
          <p>
            embedding 模型把一段文本映射成一个<strong>固定维度的稠密向量</strong>。维度<strong>由模型决定,Axon 不做额外压缩或降维</strong>,
            拿到多少维就原样存(<code>[]float32</code>)。例如 <code>text-embedding-3-small</code> 是 1536 维;你配的
            <code>text-embedding-v4</code> 按该模型的输出维度。<strong>本地兜底</strong>(关键词模式)则是固定
            <code>1024</code> 维,由字符 n-gram + 词 token 经哈希技巧(FNV-1a 取模选桶、最高位定正负号)、
            亚线性 TF 加权、再 L2 归一化得到——它是<strong>词面向量</strong>,不是神经语义。
          </p>
          <p>
            建图时,<strong>每个实体</strong>(名称+observations)和<strong>每段原文 chunk</strong> 都会被 embed 一次,
            向量随图谱缓存落盘。云端走批量接口:每批最多 <code>96</code> 条,失败对 5xx/429 线性退避重试最多 <code>3</code> 次。
          </p>

          <h4>② 查询也 embed 成同维向量</h4>
          <p>
            召回时把用户那句 query 用<strong>同一个模型</strong>embed 成向量。必须同源——若维度不一致,
            下面的余弦相似度直接返回 0(见 <code>Cosine</code> 的长度校验),这正是"切模式要重建图"的根本原因。
          </p>

          <h4>③ 余弦相似度</h4>
          <p>
            用<strong>余弦相似度</strong>衡量 query 向量和每个候选向量的接近程度:
            <code>cos = (a·b) / (‖a‖·‖b‖)</code>,取值 [-1, 1],越大越相关。
            实现里点积与两个模长都用 <code>float64</code> 累加以减小误差,任一向量为零或长度不等则返回 0。
            越接近 1 表示语义越接近。
          </p>

          <h4>④ 两个并行通道 + 各自的 top-k</h4>
          <p><strong>通道 A — 实体结构</strong>(找相关的知识节点):</p>
          <ul>
            <li>对图谱里<strong>每个带向量的实体</strong>算 query 的余弦,过滤掉低于阈值的(云端 <code>0.30</code>,本地词面向量因分布偏低用 <code>0.12</code>)。</li>
            <li>按分数降序排序,<strong>取 top-<code>8</code></strong> 作为"语义种子"(<code>seedTopK=8</code>)。这就是 top-k:全量打分 → 排序 → 截断前 k 个。</li>
            <li>再叠加<strong>关键词通道</strong>:实体名/别名在 query 里字面出现即命中。</li>
            <li>两路用 RRF 融合(见 ⑤),然后<strong>沿关系扩展一跳</strong>(<code>expandHops=1</code>,无向):把种子的直接邻居也拉进来,补上"相关但没被直接命中"的上下游节点。</li>
          </ul>
          <p><strong>通道 B — 原文片段</strong>(找可直接引用的原文):</p>
          <ul>
            <li>对<strong>每段 chunk</strong> 算余弦,过阈值(同 <code>0.30</code>/<code>0.12</code>),排序后<strong>取候选上限 <code>30</code></strong>(<code>chunkRecallCandidates</code>)。</li>
            <li>叠加关键词命中(query 拆成 ≥2 字符的 token,子串匹配)。</li>
            <li>RRF 融合后<strong>最终注入 top-<code>5</code></strong>(<code>chunkInjectTopN</code>)。</li>
          </ul>

          <h4>⑤ RRF 融合(Reciprocal Rank Fusion)</h4>
          <p>
            两个通道的分数量纲不可比(余弦 vs 是否命中),所以<strong>只看排名不看原始分</strong>。
            每个 id 的融合分 = <code>Σ 1/(k + rank)</code>,rank 从 1 开始,常数 <code>k=60</code>(业界标准值)。
            在多个列表里都靠前的条目得分最高;并列时按首次出现顺序稳定排序。这样"语义强"和"字面命中"两种信号被公平合并。
          </p>

          <h4>⑥ 无向量时怎么办</h4>
          <p>
            若 query 没能 embed(语义模式云端失败,或本就关键词模式且未建向量),两个向量通道直接熄火,
            只保留关键词/子串通道——<strong>绝不用本地向量假装成语义结果</strong>。这就是"失败不降级"在算法层的体现。
          </p>
        </div>
      </details>

      <details class="deep">
        <summary>深入:知识图谱怎么生成、怎么进化(点击展开)</summary>
        <div class="deep-body">

          <h4>建索引:从历史对话蒸馏(IndexProject)</h4>
          <p>
            读 Claude Code 在 <code>~/.claude/projects</code> 下的会话记录,<strong>逐会话</strong>处理:
          </p>
          <ul>
            <li><strong>拼接文本</strong>:把 user/assistant 消息拼成对话文本,上限 <code>16000</code> 字符;超了<strong>保留最新的轮次</strong>(结论通常在后面)。</li>
            <li><strong>LLM 蒸馏</strong>:用一个理解力强的模型(<code>gpt-5.6-sol</code>,温度 0.1)按固定提示词只提炼"下次还用得上"的持久知识——模块/服务/概念、关键决策及理由、踩过的坑与约束;<strong>丢弃</strong>寒暄、日志、报错、一次性操作、被否定的方案。强制输出严格 JSON:<code>entities</code>(name/type/aliases/observations)+ <code>relations</code>(from/label/to)。</li>
            <li><strong>溯源打戳</strong>:每条 observation 记下来自哪个会话 id,后续注入时能显示"这条知识来自 xx 会话"。</li>
            <li><strong>原文分块</strong>:把整段会话切成 chunk 存入"原文通道",连同实体一起 embedding。</li>
            <li><strong>增量 + 缓存</strong>:结果按会话存成 cache 文件。再次建索引<strong>只处理新增/改动</strong>的会话(按文件 mtime 判断);已缓存的直接跳过,不重复烧 token。若只是缓存 schema 升级,会复用旧的蒸馏结果、只补新字段。</li>
          </ul>

          <h4>从代码建图:静态骨架 + 业务富化(BuildGraphFromCode)</h4>
          <ul>
            <li><strong>静态骨架(免费、确定性、全仓)</strong>:扫描仓库,语言无关层抽出 目录/文件/包 实体和 import(<code>依赖</code>)关系;Go 文件另用 <code>go/ast</code> 精确抽 函数/类型 实体与 <code>包含</code>/<code>调用</code> 关系。文件名的 camelCase/snake_case 会拆成别名(如 <code>PaymentService</code> → "payment service"),方便自然语言命中。</li>
            <li><strong>LLM 业务富化(可选、限额)</strong>:挑最多 <code>40</code> 个关键文件,让模型<strong>只补业务含义</strong>(职责 observation + 中文/领域别名),不复述语法。名字与骨架实体完全对齐,这样能<strong>叠加</strong>到同一节点上。没配 OpenAI provider 就跳过、退化为纯骨架,不报错。</li>
            <li><strong>原文通道</strong>:每个源文件按声明边界切块 embedding,让最终代码可被逐字召回。</li>
            <li>整个代码知识存在<strong>专属 cache key</strong>(<code>code:graph</code>)下,每次重建全量覆盖。</li>
          </ul>

          <h4>利用 Obsidian 已有笔记(BuildGraphFromObsidian)</h4>
          <p>
            Obsidian 笔记是<strong>人写的、高密度、已经带结构</strong>的知识,所以处理方式和对话不同——<strong>不做 LLM 蒸馏</strong>,直接映射:
          </p>
          <ul>
            <li>每篇 <code>.md</code> 笔记 → 一个 <code>note</code> 实体(名字=标题,别名=路径/文件名)。</li>
            <li>笔记里的 <code>[[wikilink]]</code> → 一条 <code>链接</code> 关系。这是<strong>人已经确认过的关系</strong>,比模型抽的更可信,直接当图谱的边。链接目标会归一化(去掉 <code>#锚点</code>、<code>|别名</code>、<code>.md</code>、路径),对齐到目标笔记的标题以便合并。</li>
            <li>笔记正文按段落切 chunk(带一段重叠)进原文通道 embedding——笔记的主要价值在正文,所以这条通道是重点。</li>
            <li>同样<strong>增量</strong>:按 mtime 跳过未改动的笔记;跳过 <code>.obsidian</code>/<code>.trash</code> 等隐藏目录和附件。</li>
          </ul>

          <h4>多来源融合:别名归一与去重(graph.Merge)</h4>
          <p>
            对话、代码、Obsidian 三个来源各自存 cache,召回前会全部 <strong>Merge</strong> 进一张图。合并的关键是<strong>别名归一</strong>:
          </p>
          <ul>
            <li>每个实体按"名字 + 所有别名"建索引。新实体只要<strong>命中任一个 key</strong>,就折叠进已有节点,而不是新建。于是 <code>支付服务</code>、<code>PaymentService</code>、<code>payment</code> 在跨来源时<strong>collapse 成同一个节点</strong>。</li>
            <li>observations 按"事实+来源"成对<strong>并集去重</strong>;别名并集;类型缺失才补。</li>
            <li>Embedding <strong>最新非空的赢</strong>——最近一次建索引产生的向量覆盖旧的。</li>
            <li>关系按 <code>(from, to, label)</code> 归一化去重。</li>
          </ul>

          <h4>进化迭代:越用越懂(回写闭环)</h4>
          <p>
            图谱不是建一次就固定。<strong>回写(writeback)</strong>让它随使用长大:
          </p>
          <ul>
            <li>一个任务被采纳后,把它的 输入 + 确认的规格 + 最终产出 拼成文本,<strong>复用同一套蒸馏提示词</strong>抽出新的实体/关系。</li>
            <li>打上 <code>task:</code> 来源戳,embedding 后存成新的 cache,再<strong>重新 Merge</strong> 进图——新学到的事实通过别名归一自动并入相关的已有节点(如关于"支付服务"的新结论直接挂到它名下)。</li>
            <li>全程 fire-and-forget:回写失败绝不影响主流程。</li>
          </ul>
          <p class="lead">
            所以"进化"= <strong>增量建索引</strong>(新对话)+ <strong>回写</strong>(任务沉淀)+ <strong>人工校正</strong>(知识视图里改/删/确认)三条路持续往同一张图里加、并靠别名归一保持一个概念一个节点。
          </p>

          <h4>人工校正:保证质量</h4>
          <p>
            自动抽取难免有噪音。知识视图支持<strong>删实体、改 observations、合并别名</strong>——去噪和纠错后,别名归一会持续生效。<strong>图谱质量比数量重要</strong>:宁可少而准。
          </p>
        </div>
      </details>

      <hr class="divider" />

      <h3>接入 Claude Code(MCP)的原理</h3>
      <p>
        点"一键接入",Axon 把随应用分发的 <code>axon-mcp</code> 程序注册进 Claude Code 的用户配置
        (<code>~/.claude.json</code> 的 <code>mcpServers</code>),只改 <code>axon-knowledge</code> 一项、保留其它不动。
        它<strong>不依赖 <code>claude</code> 命令行</strong>——因为从 Finder 启动的应用往往拿不到完整 PATH——而是直接、
        原子地读写配置文件,macOS 与 Windows 行为一致。
      </p>
      <p>接入后,agent 在会话里能调用四个工具:</p>
      <ul>
        <li><code>list_projects</code>:列出所有已建图谱的项目。</li>
        <li><code>search_knowledge</code>:给一句话,返回相关实体、事实、原文片段,带来源。</li>
        <li><code>get_entity</code>:查看某个实体的全部事实 + 关系 + 别名。</li>
        <li><code>remember_knowledge</code>:<strong>把本次对话学到的持久知识写回图谱</strong>——这是 MCP 模式下"越用越懂"的写入闭环(见下)。</li>
      </ul>

      <h3>MCP 模式下如何让图谱越用越全</h3>
      <p>
        查询(<code>search_knowledge</code>)是<strong>只读</strong>的,查再多也不会自动沉淀。让图谱在纯 MCP 使用下持续长大,靠这几条:
      </p>
      <ul>
        <li><strong>让 agent 主动写回</strong>(核心):在 <code>CLAUDE.md</code> 里写一句"重要决策/约束确认后,调用 <code>remember_knowledge</code> 记进项目图谱"。这样每次对话里学到的结论<strong>实时</strong>写回,通过别名归一并入已有实体,不用回 GUI。</li>
        <li><strong>定期重新「建索引」</strong>:你用 Claude Code 会不断在本地积累新会话文件,回 axon 点一次建索引即可<strong>增量</strong>蒸馏进图(旧的跳过)。</li>
        <li><strong>代码/笔记变了就重跑</strong>「从代码建图」「吸收 Obsidian」。</li>
        <li><strong>人工校正</strong>:知识视图里去噪、纠错、合并别名,保证质量。</li>
      </ul>

      <hr class="divider" />

      <h3>最佳实践</h3>
      <ul>
        <li><strong>让 agent 主动查</strong>:在项目的 <code>CLAUDE.md</code> 里写一句"动手前先用 axon-knowledge 查既有决策与约束",把"偶尔查"变成"每次必查"。</li>
        <li><strong>趁热沉淀</strong>:一个非显然的决策拍板后,顺手让它进图谱;事后补总补不全。</li>
        <li><strong>定期去噪</strong>:方案废了、代码删了,对应实体也清掉,别让 agent 拿过期知识干活。</li>
        <li><strong>只沉淀"代码里看不出来、忘了会重复踩坑"的东西</strong>:决策与权衡、约束、接口/字段语义、业务规则;而不是能从代码或 git log 直接得到的事实。</li>
      </ul>

      <hr class="divider" />

      <h3>数据与隐私</h3>
      <ul>
        <li>图谱/配置存于用户数据目录(macOS <code>~/Library/Application Support/axon</code>,Windows <code>%AppData%\axon</code>)。</li>
        <li>API key <strong>只存系统凭证库</strong>(Keychain / Credential Manager),不落明文。</li>
        <li>单机单用户,无云同步。关键词模式完全离线;语义模式仅把待 embedding 的文本发给你自己配置的云端服务。</li>
      </ul>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 100;
    background: color-mix(in srgb, black 55%, transparent);
    display: flex; align-items: center; justify-content: center; padding: 40px;
  }
  .panel {
    width: min(860px, 100%); max-height: 88vh; display: flex; flex-direction: column;
    background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); box-shadow: 0 20px 60px rgba(0,0,0,.4);
  }
  .head {
    display: flex; align-items: center; justify-content: space-between;
    padding: 16px 20px; border-bottom: 1px solid var(--border);
  }
  .title { font-family: var(--font-mono); font-weight: 700; color: var(--accent); letter-spacing: .5px; }
  .close {
    background: transparent; border: none; color: var(--text-muted);
    font-size: 15px; cursor: pointer; padding: 4px 8px; border-radius: var(--radius-control);
  }
  .close:hover { color: var(--text-primary); background: var(--bg-elevated); }
  .body { overflow-y: auto; padding: 22px 26px; line-height: 1.75; font-size: 13.5px; }

  .body :global(h3) { font-size: 15px; margin: 22px 0 8px; color: var(--text-primary); }
  .body :global(h3:first-child) { margin-top: 0; }
  .body :global(p) { margin: 0 0 10px; color: var(--text-secondary, var(--text-primary)); }
  .body :global(.lead) { color: var(--text-muted); }
  .body :global(ul) { margin: 0 0 10px; padding-left: 20px; }
  .body :global(li) { margin: 3px 0; }
  .body :global(code) {
    font-family: var(--font-mono); font-size: 12px; color: var(--text-primary);
    background: var(--bg-elevated); padding: 1px 5px; border-radius: 4px;
  }
  .body :global(.step) {
    display: flex; gap: 10px; margin: 6px 0; align-items: baseline;
  }
  .body :global(.num) {
    flex: 0 0 22px; height: 22px; line-height: 22px; text-align: center;
    background: var(--accent); color: var(--accent-fg); border-radius: 50%;
    font-size: 12px; font-weight: 600;
  }
  .body :global(.tag) {
    display: inline-block; font-family: var(--font-mono); font-size: 11px;
    padding: 1px 7px; border-radius: 10px; border: 1px solid var(--border);
    color: var(--text-muted); margin-right: 6px;
  }
  .body :global(.divider) { height: 1px; background: var(--border); margin: 20px 0; border: none; }

  .body :global(details.deep) {
    margin: 12px 0 4px; border: 1px solid var(--border);
    border-radius: var(--radius-card); background: var(--bg-base);
  }
  .body :global(details.deep summary) {
    cursor: pointer; padding: 12px 14px; font-weight: 600; color: var(--accent);
    font-size: 13px; user-select: none; list-style-position: inside;
  }
  .body :global(details.deep[open] summary) { border-bottom: 1px solid var(--border); }
  .body :global(.deep-body) { padding: 6px 16px 14px; }
  .body :global(.deep-body h4) {
    font-size: 13.5px; margin: 16px 0 6px; color: var(--text-primary);
    font-family: var(--font-mono);
  }
  .body :global(.deep-body h4:first-child) { margin-top: 8px; }
</style>
