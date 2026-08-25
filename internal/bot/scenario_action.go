package bot

import (
	"context"

	"github.com/ReallocAll/bds-test-bot/internal/action"
)

type scenarioAction struct {
	scenario string
	state    *playerState
	heading float32
	done     bool
}

func newScenarioAction(scenario string, state *playerState, heading float32) action.Action {
	return &scenarioAction{scenario: scenario, state: state, heading: heading}
}

func (a *scenarioAction) Name() string {
	return a.scenario
}

func (a *scenarioAction) Start(context.Context) error {
	return nil
}

func (a *scenarioAction) Tick(context.Context, action.TickContext) error {
	return nil
}

func (a *scenarioAction) Done() bool {
	return a.done
}
