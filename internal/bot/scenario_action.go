package bot

import (
	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/go-gl/mathgl/mgl32"
)

func newScenarioAction(scenario string, state *playerState, heading float32) action.Action {
	switch scenario {
	case ScenarioChunkWalk:
		return NewMoveAction(state, mgl32.Vec2{0, 1}, chunkWalkStepPerTick, heading, 0)
	case ScenarioIdle:
		fallthrough
	default:
		return NewIdleAction(state)
	}
}
