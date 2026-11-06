package orchestr

import (
	"context"
	"fmt"
	"strings"

	"axon/internal/provider"
	"axon/internal/task"
)

// EnrichPrompt asks the model to expand a rough task into a structured, editable
// spec, explicitly filling in what the user forgot (implicit constraints,
// acceptance criteria, pitfalls). Output must be a single JSON object.
const EnrichPrompt = `你是资深工程师。用户会给一段潦草的任务描述，可能还带项目背景知识。
把它补全成一份结构完整、可直接派给执行者的规格。重点补齐用户没写、但执行者必须知道的信息：
隐含约束、验收标准、容易遗漏或踩坑的点。

只输出一个 JSON 对象，不要任何解释文字、不要代码围栏。字段：
- goal: 目标，一句话说清要达成什么
- background: 背景/上下文，结合给出的项目知识
- constraints: 约束数组（技术栈、兼容性、性能、安全等）
- scope: 涉及的文件或范围数组
- acceptCriteria: 验收标准数组，可检验
- missedPoints: 用户容易遗漏的点/易踩坑点数组
- steps: 建议执行步骤数组，有序

推断出来但不确定的条目，在该条目文本开头加 "(推断)"，让用户 review 时一眼看到哪些是模型补的。`

// ExecSystemPrompt frames the execution call: produce the result strictly per
// the spec. Execution is text output only (no disk writes).
const ExecSystemPrompt = `你是资深工程师，按给定规格产出结果（方案/代码/答案）。严格满足所有验收标准，注意规格里列出的约束和易踩坑点。`

// BuildEnrichMessages assembles the enrichment prompt: system rules + optional
// project background + the rough task input.
func BuildEnrichMessages(input, background string) []provider.ChatMessage {
	var u strings.Builder
	if strings.TrimSpace(background) != "" {
		u.WriteString(background)
		u.WriteString("\n\n---\n\n")
	}
	u.WriteString("任务描述:\n")
	u.WriteString(input)
	return []provider.ChatMessage{
		{Role: provider.RoleSystem, Content: EnrichPrompt},
		{Role: provider.RoleUser, Content: u.String()},
	}
}

// BuildExecMessages assembles the execution prompt from the spec, plus reviewer
// feedback and the previous result when this is a reject-and-iterate run.
func BuildExecMessages(spec task.Spec, feedback, prevResult string) []provider.ChatMessage {
	var u strings.Builder
	u.WriteString(RenderSpec(spec))
	if strings.TrimSpace(feedback) != "" {
		if strings.TrimSpace(prevResult) != "" {
			u.WriteString("\n\n---\n上一版产出:\n")
			u.WriteString(prevResult)
		}
		u.WriteString("\n\n---\n上一版被打回，请针对以下反馈重做:\n")
		u.WriteString(feedback)
	}
	return []provider.ChatMessage{
		{Role: provider.RoleSystem, Content: ExecSystemPrompt},
		{Role: provider.RoleUser, Content: u.String()},
	}
}

// RenderSpec flattens a Spec into a readable prompt block.
func RenderSpec(s task.Spec) string {
	var b strings.Builder
	if s.Goal != "" {
		fmt.Fprintf(&b, "目标: %s\n", s.Goal)
	}
	if s.Background != "" {
		fmt.Fprintf(&b, "\n背景:\n%s\n", s.Background)
	}
	writeList(&b, "约束", s.Constraints)
	writeList(&b, "涉及范围", s.Scope)
	writeList(&b, "验收标准", s.AcceptCriteria)
	writeList(&b, "易遗漏点", s.MissedPoints)
	writeList(&b, "建议步骤", s.Steps)
	return b.String()
}

// writeList appends a titled bullet list when items exist.
func writeList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
}

// collectReply drains a streaming provider into a single string. Enrichment
// needs the whole JSON reply at once, so there is nothing to stream to a UI.
func collectReply(ctx context.Context, prov provider.Provider, req provider.ChatRequest) (string, error) {
	chunks, errs := prov.Chat(ctx, req)
	var b strings.Builder
	for chunk := range chunks {
		b.WriteString(chunk.Delta)
	}
	select {
	case err := <-errs:
		if err != nil {
			return "", err
		}
	default:
	}
	return b.String(), nil
}
