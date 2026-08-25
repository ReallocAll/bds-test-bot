package scenario

import "testing"

type testAction struct { remaining int }

func (a *testAction) Tick() bool {
	if a.remaining == 0 {
		return false
	}
	a.remaining--
	return true
}

func TestRunnerAdvancesSteps(t *testing.T) {
	s := Scenario{Steps: []Step{{Action: "a"}, {Action: "b"}}}
	r := NewRunner(s, func(step Step) (Action, error) {
		return &testAction{remaining: 1}, nil
	})
	for i := 0; i < 2; i++ {
		if !r.Tick() {
			t.Fatalf("step %d did not run", i)
		}
	}
	if r.Tick() {
		t.Fatal("runner should finish")
	}
}
