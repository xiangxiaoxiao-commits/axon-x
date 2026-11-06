// Command orch is the standalone CLI front end for Axon's task orchestrator.
// It drives the same internal/orchestr engine as the desktop app, but with zero
// dependency on Wails, the GUI's config.json, or a Keychain entry: model access
// comes from environment variables first.
//
//	export ORCH_PROVIDER=openai          # or anthropic
//	export ORCH_BASE_URL=https://api.deepseek.com/v1
//	export ORCH_API_KEY=sk-...
//	export ORCH_MODEL=deepseek-chat
//
//	orch do "给订单服务加个超时重试"      # 交互式全流程
//	orch new "..." && orch enrich <id>   # 或分步走
//
// State lives in the same SQLite database as the desktop app when one exists,
// otherwise in a private orch.db under the app data directory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"axon/internal/db"
	"axon/internal/orchestr"
	"axon/internal/store/sqlite"
	"axon/internal/task"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("orch: ")
	log.SetFlags(0)

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	app, err := newCLI()
	if err != nil {
		fatalf("%v", err)
	}
	defer app.close()

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "new":
		err = app.cmdNew(args)
	case "list", "ls":
		err = app.cmdList(args)
	case "show":
		err = app.cmdShow(args)
	case "enrich":
		err = app.cmdEnrich(args)
	case "run":
		err = app.cmdRun(args)
	case "accept":
		err = app.cmdReview(args, "accept")
	case "reject":
		err = app.cmdReview(args, "reject")
	case "delete", "rm":
		err = app.cmdDelete(args)
	case "do":
		err = app.cmdDo(args)
	case "config":
		err = app.cmdConfig(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fatalf("未知命令 %q（orch help 查看用法）", cmd)
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `orch —— 任务编排命令行

用法:
  orch do "一句话描述"        交互式走完 补全 → 确认 → 执行 → 采纳/打回
  orch new "一句话描述"       只建任务（草稿）
  orch enrich <id>            补全成结构化规格
  orch show <id>              查看任务、规格与历史轮次
  orch run <id>               执行（流式输出）
  orch accept <id>            采纳结果
  orch reject <id> "反馈"     打回并按反馈重跑
  orch list                   列出所有任务（可加 --status=review_spec）
  orch delete <id>            删除任务及其历史
  orch config                 显示当前生效的模型配置

环境变量:
  ORCH_PROVIDER   openai | anthropic（默认 openai）
  ORCH_BASE_URL   接口地址，如 https://api.deepseek.com/v1
  ORCH_API_KEY    密钥
  ORCH_MODEL      模型 id
  ORCH_PROJECT    关联的知识图谱项目（可选，不给则不做知识注入）
  ORCH_DATA_DIR   数据目录（默认与 Axon 桌面版共用）
`)
}

func fatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "orch: "+format+"\n", a...)
	os.Exit(1)
}

// cli holds the wired-up engine plus the emitter that renders progress.
type cli struct {
	eng    *orchestr.Engine
	store  task.Store
	ctx    context.Context
	events *eventBus
	env    envConfig
}

func newCLI() (*cli, error) {
	env := loadEnv()

	dataDir := env.DataDir
	if dataDir == "" {
		d, err := db.AppDataDir()
		if err != nil {
			return nil, fmt.Errorf("定位数据目录失败: %w", err)
		}
		dataDir = d
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	env.DataDir = dataDir

	// Share the desktop app's database so tasks created in either front end are
	// visible in the other. db.Open applies the embedded migrations on open.
	sqlDB, err := db.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	bus := newEventBus()
	eng, err := orchestr.New(orchestr.Deps{
		Ctx:       context.Background(),
		Store:     sqlite.NewTaskStore(sqlDB),
		Provider:  env.providerFactory(),
		Recall:    newGraphRecaller(dataDir, env),
		Writeback: newGraphWritebacker(dataDir, env),
		Emit:      bus.emit,
	})
	if err != nil {
		return nil, err
	}
	eng.RecoverStaleTasks()

	return &cli{
		eng:    eng,
		store:  sqlite.NewTaskStore(sqlDB),
		ctx:    context.Background(),
		events: bus,
		env:    env,
	}, nil
}

func (c *cli) close() {}

// --- commands ---

func (c *cli) cmdNew(args []string) error {
	input := strings.TrimSpace(strings.Join(args, " "))
	if input == "" {
		return errors.New("用法: orch new \"任务描述\"")
	}
	t, err := c.eng.CreateTask(input, "", "", c.env.Project)
	if err != nil {
		return err
	}
	fmt.Printf("已建任务 %s（草稿）\n  标题: %s\n", t.ID, t.Title)
	fmt.Printf("下一步: orch enrich %s\n", t.ID)
	return nil
}

func (c *cli) cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	status := fs.String("status", "", "只看某个状态")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tasks, err := c.eng.ListTasks()
	if err != nil {
		return err
	}
	shown := 0
	for _, t := range tasks {
		if *status != "" && string(t.Status) != *status {
			continue
		}
		shown++
		fmt.Printf("%-8s  %-14s  %s\n", t.ID, t.Status, t.Title)
	}
	if shown == 0 {
		fmt.Println("（没有任务）")
	}
	return nil
}

func (c *cli) cmdShow(args []string) error {
	if len(args) < 1 {
		return errors.New("用法: orch show <id>")
	}
	t, err := c.eng.GetTask(args[0])
	if err != nil {
		return err
	}
	printTask(t)
	runs, _ := c.eng.ListRuns(t.ID)
	for _, r := range runs {
		fmt.Printf("\n─── 第 %d 轮 ───\n", r.Seq)
		if r.Feedback != "" {
			fmt.Printf("打回反馈: %s\n\n", r.Feedback)
		}
		fmt.Println(r.Result)
	}
	return nil
}

func (c *cli) cmdEnrich(args []string) error {
	if len(args) < 1 {
		return errors.New("用法: orch enrich <id>")
	}
	id := args[0]
	if err := c.eng.EnrichTask(id); err != nil {
		return err
	}
	if _, err := c.waitFor(id, task.StatusReviewSpec); err != nil {
		return err
	}
	t, err := c.eng.GetTask(id)
	if err != nil {
		return err
	}
	printTask(t)
	if t.FailedStage == "enrich" {
		fmt.Println("\n注意: 模型没有返回可解析的 JSON，规格为空。可以直接编辑规格文件后重跑。")
	}
	return nil
}

func (c *cli) cmdRun(args []string) error {
	if len(args) < 1 {
		return errors.New("用法: orch run <id>")
	}
	id := args[0]
	t, err := c.eng.GetTask(id)
	if err != nil {
		return err
	}
	if t.Status != task.StatusReviewSpec {
		return fmt.Errorf("任务当前是 %s，只有 review_spec 状态才能执行", t.Status)
	}
	fmt.Println("──────── 产出 ────────")
	if err := c.runAndWait(id); err != nil {
		return err
	}
	fmt.Print("\n")
	fmt.Printf("下一步: orch accept %s  或  orch reject %s \"哪里不对\"\n", id, id)
	return nil
}

func (c *cli) cmdReview(args []string, decision string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法: orch %s <id> [反馈]", decision)
	}
	id := args[0]
	feedback := strings.TrimSpace(strings.Join(args[1:], " "))
	if decision == "accept" {
		if err := c.eng.ReviewTask(id, "accept", ""); err != nil {
			return err
		}
		fmt.Printf("已采纳 %s", id)
		// Use the task's own project binding, not the current shell's: the binding
		// was fixed at creation time.
		if t, err := c.eng.GetTask(id); err == nil && t.ProjectSlug != "" {
			// The engine writes back in the background; a CLI exits immediately
			// after this, so wait for it or the knowledge is lost.
			fmt.Printf("，正在回写图谱 %q…", t.ProjectSlug)
			if !c.eng.WaitBackground(30 * time.Second) {
				fmt.Printf("\n（回写超时，知识可能未落盘；用 orch show %s 可确认任务已采纳）", id)
			}
		}
		fmt.Println()
		return nil
	}
	if feedback == "" {
		return fmt.Errorf("打回需要给出反馈: orch reject %s \"哪里不对\"", id)
	}
	fmt.Println("──────── 重跑产出 ────────")
	return c.runAndWait(id, feedback)
}

func (c *cli) cmdDelete(args []string) error {
	if len(args) < 1 {
		return errors.New("用法: orch delete <id>")
	}
	if err := c.eng.DeleteTask(args[0]); err != nil {
		return err
	}
	fmt.Printf("已删除 %s\n", args[0])
	return nil
}

func (c *cli) cmdConfig(args []string) error {
	fmt.Printf("provider: %s\n", orDefault(c.env.Provider, "openai"))
	fmt.Printf("baseURL : %s\n", orDefault(c.env.BaseURL, "(provider 默认)"))
	fmt.Printf("model   : %s\n", orDefault(c.env.Model, "(未设置)"))
	fmt.Printf("apiKey  : %s\n", maskKey(c.env.APIKey))
	fmt.Printf("project : %s\n", orDefault(c.env.Project, "(未关联，不做知识注入)"))
	fmt.Printf("dataDir : %s\n", c.env.DataDir)
	return nil
}

// runAndWait submits a run (new task exec, or a reject-iterate when feedback is
// given) and blocks until the engine reports the outcome.
func (c *cli) runAndWait(id string, feedback ...string) error {
	fb := ""
	if len(feedback) > 0 {
		fb = feedback[0]
	}
	var err error
	if fb == "" {
		err = c.eng.RunTask(id)
	} else {
		err = c.eng.ReviewTask(id, "reject", fb)
	}
	if err != nil {
		return err
	}
	// A cancelled run reverts to review_spec rather than review_result, so accept
	// both as terminal outcomes here.
	_, err = c.waitFor(id, task.StatusReviewResult, task.StatusReviewSpec)
	return err
}

// waitFor polls task state until it reaches one of wants, fails, or times out.
//
// Polling rather than subscribing to events is deliberate: the engine starts
// enrichment in a goroutine that can finish before a subscriber registers, so an
// event-only handshake would race on fast replies.
func (c *cli) waitFor(id string, wants ...task.Status) (task.Task, error) {
	// Generous ceiling: a slow model can legitimately take minutes on 4000 tokens.
	deadline := time.Now().Add(15 * time.Minute)
	for {
		t, err := c.eng.GetTask(id)
		if err != nil {
			return t, err
		}
		for _, w := range wants {
			if t.Status == w {
				return t, nil
			}
		}
		if t.Status == task.StatusFailed {
			return t, fmt.Errorf("任务失败（%s 阶段）: %s", t.FailedStage, c.events.takeErr(id))
		}
		if time.Now().After(deadline) {
			return t, fmt.Errorf("等待超时，任务仍处于 %s（可用 orch show %s 查看）", t.Status, id)
		}
		time.Sleep(80 * time.Millisecond)
	}
}

// --- rendering ---

func printTask(t task.Task) {
	fmt.Printf("任务 %s  [%s]\n", t.ID, t.Status)
	fmt.Printf("标题: %s\n", t.Title)
	fmt.Printf("\n原始输入:\n%s\n", indent(t.Input))
	if t.Spec.Goal == "" && len(t.Spec.AcceptCriteria) == 0 {
		fmt.Printf("\n规格: （空，尚未补全或解析失败）\n")
		return
	}
	s := t.Spec
	fmt.Printf("\n目标: %s\n", s.Goal)
	if s.Background != "" {
		fmt.Printf("\n背景:\n%s\n", indent(s.Background))
	}
	printList("约束", s.Constraints)
	printList("涉及范围", s.Scope)
	printList("验收标准", s.AcceptCriteria)
	printList("易遗漏点", s.MissedPoints)
	printList("建议步骤", s.Steps)
	if len(s.InjectedKnowledge) > 0 {
		fmt.Printf("\n注入的项目知识: %s", strings.Join(s.InjectedKnowledge, "、"))
		if s.RecallMethod != "" {
			fmt.Printf("（召回方式: %s）", s.RecallMethod)
		}
		fmt.Println()
	}
	if t.FailedStage != "" {
		fmt.Printf("\n失败阶段: %s\n", t.FailedStage)
	}
}

func printList(title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("\n%s:\n", title)
	for _, it := range items {
		fmt.Printf("  - %s\n", it)
	}
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func maskKey(k string) string {
	if k == "" {
		return "(未设置)"
	}
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "..." + k[len(k)-4:]
}

// --- interactive flow ---

// cmdDo runs the whole loop for a one-line description: enrich, show the spec,
// ask for confirmation, execute, then accept or iterate.
func (c *cli) cmdDo(args []string) error {
	input := strings.TrimSpace(strings.Join(args, " "))
	if input == "" {
		return errors.New("用法: orch do \"一句话描述\"")
	}
	t, err := c.eng.CreateTask(input, "", "", c.env.Project)
	if err != nil {
		return err
	}
	fmt.Printf("任务 %s 已建立，正在补全规格…\n", t.ID)
	if err := c.eng.EnrichTask(t.ID); err != nil {
		return err
	}
	if _, err := c.waitFor(t.ID, task.StatusReviewSpec); err != nil {
		return err
	}
	t, _ = c.eng.GetTask(t.ID)
	printTask(t)

	for {
		fmt.Print("\n确认这份规格并执行? [y=执行 / n=放弃 / r=重新补全] ")
		switch strings.ToLower(readLine()) {
		case "y", "yes":
			fmt.Println("\n──────── 产出 ────────")
			if err := c.runAndWait(t.ID); err != nil {
				return err
			}
			return c.reviewLoop(t.ID)
		case "n":
			fmt.Printf("已放弃，任务保留为草稿: %s\n", t.ID)
			return nil
		case "r":
			fmt.Println("重新补全中…")
			if err := c.eng.EnrichTask(t.ID); err != nil {
				return err
			}
			if _, err := c.waitFor(t.ID, task.StatusReviewSpec); err != nil {
				return err
			}
			t, _ = c.eng.GetTask(t.ID)
			printTask(t)
		default:
			fmt.Println("请输入 y / n / r")
		}
	}
}

// reviewLoop accepts the result or iterates with feedback.
func (c *cli) reviewLoop(id string) error {
	for {
		fmt.Print("\n采纳这份产出? [y=采纳 / 直接输入反馈=打回重跑 / q=先放着] ")
		line := readLine()
		switch {
		case line == "y", line == "yes":
			if err := c.eng.ReviewTask(id, "accept", ""); err != nil {
				return err
			}
			fmt.Printf("已采纳 %s", id)
			if t, err := c.eng.GetTask(id); err == nil && t.ProjectSlug != "" {
				fmt.Printf("，正在回写图谱 %q…", t.ProjectSlug)
				c.eng.WaitBackground(30 * time.Second)
			}
			fmt.Println()
			return nil
		case line == "q", line == "":
			fmt.Printf("先放着了，稍后可 orch accept %s 或 orch reject %s \"反馈\"\n", id, id)
			return nil
		default:
			fmt.Println("\n──────── 重跑产出 ────────")
			if err := c.runAndWait(id, line); err != nil {
				return err
			}
		}
	}
}

// readLine reads one line from stdin, tolerating a closed pipe.
func readLine() string {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			if buf[0] != '\r' {
				b.WriteByte(buf[0])
			}
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
