package bot

import (
	"context"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/go-gl/mathgl/mgl32"
)

// IdleAction keeps a bot stationary while preserving server-corrected rotation.
type IdleAction struct {
	state *playerState
}

func NewIdleAction(state *playerState) *IdleAction {
	return &IdleAction{state: state}
}

func (a *IdleAction) Name() string { return "idle" }

func (a *IdleAction) Start(context.Context) error {
	a.state.setIdleControl()
	return nil
}

func (a *IdleAction) Tick(context.Context, action.TickContext) error {
	a.state.setIdleControl()
	return nil
}

func (a *IdleAction) Done() bool { return false }

// MoveAction applies a local movement vector at a fixed yaw for a number of client ticks.
// A duration of zero means the action runs until the bot is stopped.
type MoveAction struct {
	state         *playerState
	vector        mgl32.Vec2
	stepPerTick   float32
	yaw           float32
	durationTicks uint64
	elapsed       uint64
	done          bool
}

func NewMoveAction(state *playerState, vector mgl32.Vec2, stepPerTick, yaw float32, durationTicks uint64) *MoveAction {
	return &MoveAction{
		state:         state,
		vector:        vector,
		stepPerTick:   stepPerTick,
		yaw:           yaw,
		durationTicks: durationTicks,
	}
}

func (a *MoveAction) Name() string { return "move" }

func (a *MoveAction) Start(context.Context) error {
	a.state.setMoveControl(a.vector, a.stepPerTick, a.yaw)
	return nil
}

func (a *MoveAction) Tick(context.Context, action.TickContext) error {
	if a.done {
		return nil
	}
	if a.durationTicks > 0 && a.elapsed >= a.durationTicks {
		a.state.clearMoveControl()
		a.done = true
		return nil
	}
	a.state.setMoveControl(a.vector, a.stepPerTick, a.yaw)
	a.elapsed++
	return nil
}

func (a *MoveAction) Done() bool { return a.done }

// LookAction holds a pitch/yaw view direction for a number of client ticks.
// A duration of zero means the action runs until the bot is stopped.
type LookAction struct {
	state         *playerState
	pitch         float32
	yaw           float32
	durationTicks uint64
	elapsed       uint64
	done          bool
}

func NewLookAction(state *playerState, pitch, yaw float32, durationTicks uint64) *LookAction {
	return &LookAction{state: state, pitch: pitch, yaw: yaw, durationTicks: durationTicks}
}

func (a *LookAction) Name() string { return "look" }

func (a *LookAction) Start(context.Context) error {
	a.state.setLookControl(a.pitch, a.yaw)
	return nil
}

func (a *LookAction) Tick(context.Context, action.TickContext) error {
	if a.done {
		return nil
	}
	if a.durationTicks > 0 && a.elapsed >= a.durationTicks {
		a.state.clearLookControl()
		a.done = true
		return nil
	}
	a.state.setLookControl(a.pitch, a.yaw)
	a.elapsed++
	return nil
}

func (a *LookAction) Done() bool { return a.done }

// JumpAction holds the Bedrock jump input for durationTicks client ticks and then
// emits a release transition on the following input packet.
type JumpAction struct {
	state         *playerState
	durationTicks uint64
	elapsed       uint64
	done          bool
}

func NewJumpAction(state *playerState, durationTicks uint64) *JumpAction {
	if durationTicks == 0 {
		durationTicks = 1
	}
	return &JumpAction{state: state, durationTicks: durationTicks}
}

func (a *JumpAction) Name() string { return "jump" }

func (a *JumpAction) Start(context.Context) error {
	a.state.setJumpControl(true)
	return nil
}

func (a *JumpAction) Tick(context.Context, action.TickContext) error {
	if a.done {
		return nil
	}
	if a.elapsed >= a.durationTicks {
		a.state.setJumpControl(false)
		a.done = true
		return nil
	}
	a.state.setJumpControl(true)
	a.elapsed++
	return nil
}

func (a *JumpAction) Done() bool { return a.done }
