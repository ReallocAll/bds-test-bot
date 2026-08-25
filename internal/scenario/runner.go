package scenario

import "fmt"

// Runner executes scenario steps through an action factory.
type Runner struct {
	steps   []Step
	index   int
	repeat  int
	current Action
	factory ActionFactory
	err     error
}

// Action is the minimal executable unit produced by a scenario step.
type Action interface {
	Tick() bool
}

// ActionFactory creates runtime actions from scenario steps.
type ActionFactory func(Step) (Action, error)

func NewRunner(s Scenario, factory ActionFactory) *Runner {
	return &Runner{steps: s.Steps, factory: factory}
}

// Tick advances the current scenario. It returns false when all steps are done
// or execution has failed. Call Err to distinguish those states.
func (r *Runner) Tick() bool {
	if r.err != nil {
		return false
	}

	for {
		if r.current == nil {
			if r.index >= len(r.steps) {
				return false
			}

			step := r.steps[r.index]
			action, err := r.factory(step)
			if err != nil {
				r.err = fmt.Errorf("%w at step %d (%s): %v", ErrActionFactory, r.index, step.Action, err)
				return false
			}
			if action == nil {
				r.err = fmt.Errorf("%w at step %d (%s): nil action", ErrActionFactory, r.index, step.Action)
				return false
			}
			r.current = action
			if r.repeat == 0 {
				r.repeat = step.Repeat
			}
		}

		if r.current.Tick() {
			return true
		}

		r.current = nil
		if r.repeat > 1 {
			r.repeat--
			continue
		}

		r.repeat = 0
		r.index++
	}
}

// Err returns the terminal execution error, if any.
func (r *Runner) Err() error { return r.err }
