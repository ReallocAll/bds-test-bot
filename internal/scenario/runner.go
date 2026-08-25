package scenario

import (
	"context"
	"fmt"

	"github.com/ReallocAll/bds-test-bot/internal/action"
)

// Runner executes a parsed scenario as an action.Action so it can be plugged
// directly into the bot's existing 20 TPS action runner.
type Runner struct {
	scenario Scenario
	factory  ActionFactory
	index    int
	repeat   int
	current  action.Action
	started  bool
	done     bool
	err      error
}

// ActionFactory creates a concrete bot action from one scenario step.
type ActionFactory func(Step) (action.Action, error)

func NewRunner(s Scenario, factory ActionFactory) *Runner {
	return &Runner{scenario: s, factory: factory}
}

func (r *Runner) Name() string {
	if r.scenario.Name == "" {
		return "scenario"
	}
	return "scenario:" + r.scenario.Name
}

func (r *Runner) Start(ctx context.Context) error {
	if r.started {
		return r.err
	}
	r.started = true
	if len(r.scenario.Steps) == 0 {
		r.done = true
		return nil
	}
	return r.startCurrent(ctx)
}

func (r *Runner) Tick(ctx context.Context, tick action.TickContext) error {
	if r.err != nil {
		return r.err
	}
	if r.done {
		return nil
	}
	if !r.started {
		if err := r.Start(ctx); err != nil {
			return err
		}
	}
	if r.done {
		return nil
	}

	if err := r.current.Tick(ctx, tick); err != nil {
		r.err = fmt.Errorf("scenario step %d (%s): %w", r.index, r.scenario.Steps[r.index].Action, err)
		return r.err
	}
	if !r.current.Done() {
		return nil
	}

	if r.repeat > 1 {
		r.repeat--
		return r.replaceCurrent(ctx)
	}

	r.index++
	if r.index >= len(r.scenario.Steps) {
		r.current = nil
		r.done = true
		return nil
	}
	return r.startCurrent(ctx)
}

func (r *Runner) Done() bool { return r.done }

// Err returns the terminal scenario error, if any.
func (r *Runner) Err() error { return r.err }

func (r *Runner) startCurrent(ctx context.Context) error {
	step := r.scenario.Steps[r.index]
	r.repeat = step.Repeat
	if r.repeat == 0 {
		r.repeat = 1
	}
	return r.replaceCurrent(ctx)
}

func (r *Runner) replaceCurrent(ctx context.Context) error {
	step := r.scenario.Steps[r.index]
	current, err := r.factory(step)
	if err != nil {
		r.err = fmt.Errorf("%w at step %d (%s): %v", ErrActionFactory, r.index, step.Action, err)
		return r.err
	}
	if current == nil {
		r.err = fmt.Errorf("%w at step %d (%s): nil action", ErrActionFactory, r.index, step.Action)
		return r.err
	}
	r.current = current
	if err := r.current.Start(ctx); err != nil {
		r.err = fmt.Errorf("scenario step %d (%s) start: %w", r.index, step.Action, err)
		return r.err
	}
	return nil
}
