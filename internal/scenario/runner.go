package scenario

// Runner executes scenario steps through an action factory.
type Runner struct {
	steps    []Step
	index    int
	current  Action
	factory  ActionFactory
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

// Tick advances the current scenario. It returns false when all steps are done.
func (r *Runner) Tick() bool {
	for {
		if r.current == nil {
			if r.index >= len(r.steps) {
				return false
			}
			action, err := r.factory(r.steps[r.index])
			if err != nil {
				return false
			}
			r.current = action
		}

		if r.current.Tick() {
			return true
		}

		r.current = nil
		r.index++
	}
}
