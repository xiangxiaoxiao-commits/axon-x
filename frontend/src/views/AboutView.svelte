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

      <hr class="divider" />

      <h3>接入 Claude Code(MCP)的原理</h3>
      <p>
        点"一键接入",Axon 把随应用分发的 <code>axon-mcp</code> 程序注册进 Claude Code 的用户配置
        (<code>~/.claude.json</code> 的 <code>mcpServers</code>),只改 <code>axon-knowledge</code> 一项、保留其它不动。
        它<strong>不依赖 <code>claude</code> 命令行</strong>——因为从 Finder 启动的应用往往拿不到完整 PATH——而是直接、
        原子地读写配置文件,macOS 与 Windows 行为一致。
      </p>
      <p>接入后,agent 在会话里能调用三个工具:</p>
      <ul>
        <li><code>list_projects</code>:列出所有已建图谱的项目。</li>
        <li><code>search_knowledge</code>:给一句话,返回相关实体、事实、原文片段,带来源。</li>
        <li><code>get_entity</code>:查看某个实体的全部事实 + 关系 + 别名。</li>
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
</style>
