package scenario

import (
	"context"
	"errors"
	"testing"

	"github.com/ReallocAll/bds-test-bot/internal/action"
)

func TestRunnerExposesFactoryError(t *testing.T) {
	want := errors.New("boom")
	r := NewRunner(Scenario{Name: "bad", Steps: []Step{{Action: "broken"}}}, func(Step) (action.Action, error) {
		return nil, want
	})
	if err := r.Start(context.Background()); !errors.Is(err, ErrActionFactory) {
		t.Fatalf("Start() error = %v, want ErrActionFactory", err)
	}
	if !errors.Is(r.Err(), ErrActionFactory) {
		t.Fatalf("Err() = %v, want ErrActionFactory", r.Err())
	}
}

func TestRunnerRejectsNilAction(t *testing.T) {
	r := NewRunner(Scenario{Name: "bad", Steps: []Step{{Action: "nil"}}}, func(Step) (action.Action, error) {
		return nil, nil
	})
	if err := r.Start(context.Background()); !errors.Is(err, ErrActionFactory) {
		t.Fatalf("Start() error = %v, want ErrActionFactory", err)
	}
	if !errors.Is(r.Err(), ErrActionFactory) {
		t.Fatalf("Err() = %v, want ErrActionFactory", r.Err())
	}
}
