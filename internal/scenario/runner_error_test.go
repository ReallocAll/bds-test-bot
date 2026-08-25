package scenario

import (
	"errors"
	"testing"
)

func TestRunnerExposesFactoryError(t *testing.T) {
	want := errors.New("boom")
	r := NewRunner(Scenario{Name: "bad", Steps: []Step{{Action: "broken"}}}, func(Step) (Action, error) {
		return nil, want
	})
	if r.Tick() {
		t.Fatal("runner should stop after factory error")
	}
	if !errors.Is(r.Err(), ErrActionFactory) {
		t.Fatalf("Err() = %v, want ErrActionFactory", r.Err())
	}
}

func TestRunnerRejectsNilAction(t *testing.T) {
	r := NewRunner(Scenario{Name: "bad", Steps: []Step{{Action: "nil"}}}, func(Step) (Action, error) {
		return nil, nil
	})
	if r.Tick() {
		t.Fatal("runner should stop for nil action")
	}
	if !errors.Is(r.Err(), ErrActionFactory) {
		t.Fatalf("Err() = %v, want ErrActionFactory", r.Err())
	}
}
