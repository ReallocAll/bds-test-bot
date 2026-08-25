package scenario

import (
	"context"
	"testing"

	"github.com/ReallocAll/bds-test-bot/internal/action"
)

type testAction struct {
	remaining int
	done      bool
}

func (a *testAction) Name() string { return "test" }
func (a *testAction) Start(context.Context) error { return nil }
func (a *testAction) Tick(context.Context, action.TickContext) error {
	if a.done {
		return nil
	}
	if a.remaining > 0 {
		a.remaining--
	}
	if a.remaining == 0 {
		a.done = true
	}
	return nil
}
func (a *testAction) Done() bool { return a.done }

func TestRunnerAdvancesSteps(t *testing.T) {
	s := Scenario{Name: "sequence", Steps: []Step{{Action: "a"}, {Action: "b"}}}
	r := NewRunner(s, func(step Step) (action.Action, error) {
		return &testAction{remaining: 1}, nil
	})
	ctx := context.Background()
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for tick := uint64(1); tick <= 2; tick++ {
		if err := r.Tick(ctx, action.TickContext{Tick: tick}); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	if !r.Done() {
		t.Fatal("runner should finish after both steps")
	}
}
