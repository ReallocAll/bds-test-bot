package scenario

import "testing"

type countAction struct { ticks int }

func (a *countAction) Tick() bool {
	a.ticks++
	return a.ticks < 1
}

func TestRunnerRepeat(t *testing.T) {
	runner := NewRunner(Scenario{
		Name: "repeat",
		Steps: []Step{{Action: "test", Repeat: 3}},
	}, func(Step) (Action, error) {
		return &countAction{}, nil
	})

	count := 0
	for runner.Tick() {
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 executions, got %d", count)
	}
}
