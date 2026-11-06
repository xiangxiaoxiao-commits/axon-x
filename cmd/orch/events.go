package main

import (
	"fmt"
	"sync"

	"axon/internal/orchestr"
)

// eventBus renders streamed output and remembers per-task errors.
//
// Waiting on completion is done by polling task state (see cli.waitFor) rather
// than by subscribing to events: the engine runs enrichment in a goroutine that
// can finish before a subscriber registers, so an event-only handshake would
// race. Events are used purely for live rendering.
type eventBus struct {
	mu   sync.Mutex
	errs map[string]string
}

func newEventBus() *eventBus {
	return &eventBus{errs: make(map[string]string)}
}

func (b *eventBus) emit(event string, data ...interface{}) {
	switch event {
	case orchestr.EventTaskDelta:
		if len(data) == 0 {
			return
		}
		if ev, ok := data[0].(orchestr.TaskDeltaEvent); ok && ev.Delta != "" {
			fmt.Print(ev.Delta)
		}
	case orchestr.EventTaskError:
		if len(data) == 0 {
			return
		}
		if ev, ok := data[0].(orchestr.TaskErrorEvent); ok {
			b.mu.Lock()
			b.errs[ev.TaskID] = fmt.Sprintf("%s 阶段: %s", ev.Stage, ev.Error)
			b.mu.Unlock()
		}
	}
}

// takeErr returns and clears the last error recorded for a task.
func (b *eventBus) takeErr(id string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.errs[id]
	delete(b.errs, id)
	if e == "" {
		return "模型没有返回具体错误信息"
	}
	return e
}
