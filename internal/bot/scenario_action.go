package bot

import (
	"fmt"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	scenarioengine "github.com/ReallocAll/bds-test-bot/internal/scenario"
	"github.com/go-gl/mathgl/mgl32"
)

func newScenarioAction(cfg Config, state *playerState, heading float32) (action.Action, error) {
	if cfg.ScenarioDefinition != nil {
		return scenarioengine.NewRunner(*cfg.ScenarioDefinition, func(step scenarioengine.Step) (action.Action, error) {
			return newConfiguredAction(step, state, heading)
		}), nil
	}

	switch cfg.Scenario {
	case ScenarioChunkWalk:
		return NewMoveAction(state, mgl32.Vec2{0, 1}, chunkWalkStepPerTick, heading, 0), nil
	case ScenarioIdle:
		return NewIdleAction(state), nil
	default:
		return nil, fmt.Errorf("unsupported scenario %q", cfg.Scenario)
	}
}

func newConfiguredAction(step scenarioengine.Step, state *playerState, heading float32) (action.Action, error) {
	switch step.Action {
	case scenarioengine.ActionIdle:
		if step.Ticks > 0 {
			return action.NewTimed("idle", uint64(step.Ticks), func(action.TickContext) { state.setIdleControl() }), nil
		}
		return NewIdleAction(state), nil
	case scenarioengine.ActionWait:
		return action.NewTimed("wait", uint64(step.Ticks), func(action.TickContext) { state.setIdleControl() }), nil
	case scenarioengine.ActionMove:
		yaw := heading
		if step.Yaw != nil {
			yaw = *step.Yaw
		}
		speed := step.Speed
		if speed == 0 {
			speed = chunkWalkStepPerTick
		}
		return NewMoveAction(state, mgl32.Vec2{step.Strafe, step.Forward}, speed, yaw, uint64(step.Ticks)), nil
	case scenarioengine.ActionLook:
		pitch := float32(0)
		yaw := heading
		if step.Pitch != nil {
			pitch = *step.Pitch
		}
		if step.Yaw != nil {
			yaw = *step.Yaw
		}
		return NewLookAction(state, pitch, yaw, uint64(step.Ticks)), nil
	case scenarioengine.ActionJump:
		return NewJumpAction(state, uint64(step.Ticks)), nil
	default:
		return nil, fmt.Errorf("unsupported configured action %q", step.Action)
	}
}
