package scenario

import (
	"context"
	"testing"

	"github.com/ReallocAll/bds-test-bot/internal/action"
)

func TestRunnerRepeat(t *testing.T) {
	created := 0
	runner := NewRunner(Scenario{
		Name:  "repeat",
		Steps: []Step{{Action: "test", Repeat: 3}},
	}, func(Step) (action.Action, error) {
		created++
		return &testAction{remaining: 1}, nil
	})

	ctx := context.Background()
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for tick := uint64(1); tick <= 3; tick++ {
		if err := runner.Tick(ctx, action.TickContext{Tick: tick}); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	if !runner.Done() {
		t.Fatal("runner should finish after three repetitions")
	}
	if created != 3 {
		t.Fatalf("factory calls = %d, want 3", created)
	}
}
