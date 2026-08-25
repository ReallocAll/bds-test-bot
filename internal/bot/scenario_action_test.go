package bot

import (
	"context"
	"testing"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	scenarioengine "github.com/ReallocAll/bds-test-bot/internal/scenario"
	"github.com/go-gl/mathgl/mgl32"
)

func TestConfiguredScenarioMovesThenWaits(t *testing.T) {
	definition := scenarioengine.Scenario{
		Name: "mixed",
		Steps: []scenarioengine.Step{
			{Action: scenarioengine.ActionMove, Ticks: 1, Forward: 1},
			{Action: scenarioengine.ActionWait, Ticks: 1},
		},
	}
	cfg := DefaultConfig()
	cfg.Scenario = definition.Name
	cfg.ScenarioDefinition = &definition
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)

	scenarioAction, err := newScenarioAction(cfg, state, 0)
	if err != nil {
		t.Fatal(err)
	}
	runner := action.NewRunner(scenarioAction)
	ctx := context.Background()
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runner.Tick(ctx, action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	moving := authInputPacket(state, 1)
	if moving.MoveVector[1] <= 0 || moving.Delta == (mgl32.Vec3{}) {
		t.Fatalf("configured move did not produce movement: %+v", moving)
	}
	if err := runner.Tick(ctx, action.TickContext{Tick: 2}); err != nil {
		t.Fatal(err)
	}
	waiting := authInputPacket(state, 2)
	if waiting.MoveVector != (mgl32.Vec2{}) || waiting.Delta != (mgl32.Vec3{}) {
		t.Fatalf("wait step did not clear movement: %+v", waiting)
	}
}

func TestConfiguredActionFactorySupportsLookAndJump(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{}, 0, 0)
	yaw := float32(90)
	pitch := float32(-15)
	look, err := newConfiguredAction(scenarioengine.Step{Action: scenarioengine.ActionLook, Ticks: 2, Yaw: &yaw, Pitch: &pitch}, state, 0)
	if err != nil || look.Name() != "look" {
		t.Fatalf("look action = %v, %v", look, err)
	}
	jump, err := newConfiguredAction(scenarioengine.Step{Action: scenarioengine.ActionJump, Ticks: 2}, state, 0)
	if err != nil || jump.Name() != "jump" {
		t.Fatalf("jump action = %v, %v", jump, err)
	}
}
