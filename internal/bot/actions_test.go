package bot

import (
	"context"
	"math"
	"testing"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestMoveActionAppliesAndStops(t *testing.T) {
	ctx := context.Background()
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	move := NewMoveAction(state, mgl32.Vec2{0, 1}, chunkWalkStepPerTick, 0, 2)
	if err := move.Start(ctx); err != nil {
		t.Fatal(err)
	}

	for tick := uint64(1); tick <= 2; tick++ {
		if err := move.Tick(ctx, action.TickContext{Tick: tick}); err != nil {
			t.Fatal(err)
		}
		pk := authInputPacket(state, tick)
		if pk.MoveVector != (mgl32.Vec2{0, 1}) {
			t.Fatalf("tick %d move vector = %v", tick, pk.MoveVector)
		}
	}

	if err := move.Tick(ctx, action.TickContext{Tick: 3}); err != nil {
		t.Fatal(err)
	}
	if !move.Done() {
		t.Fatal("move action should be done after its duration")
	}
	if pk := authInputPacket(state, 3); pk.MoveVector != (mgl32.Vec2{}) {
		t.Fatalf("move action did not clear movement: %v", pk.MoveVector)
	}
}

func TestLookActionOverridesAndReleasesRotation(t *testing.T) {
	ctx := context.Background()
	state := newPlayerState(mgl32.Vec3{}, 5, 10)
	look := NewLookAction(state, -20, 90, 1)
	if err := look.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := look.Tick(ctx, action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	pk := authInputPacket(state, 1)
	if pk.Pitch != -20 || pk.Yaw != 90 || pk.HeadYaw != 90 {
		t.Fatalf("look action rotation = pitch %f yaw %f head %f", pk.Pitch, pk.Yaw, pk.HeadYaw)
	}
	wantX := -math.Cos(20 * math.Pi / 180)
	wantY := math.Sin(20 * math.Pi / 180)
	if math.Abs(float64(pk.CameraOrientation[0])-wantX) > 1e-5 || math.Abs(float64(pk.CameraOrientation[1])-wantY) > 1e-5 || math.Abs(float64(pk.CameraOrientation[2])) > 1e-5 {
		t.Fatalf("unexpected camera orientation: %v", pk.CameraOrientation)
	}

	if err := look.Tick(ctx, action.TickContext{Tick: 2}); err != nil {
		t.Fatal(err)
	}
	pk = authInputPacket(state, 2)
	if pk.Pitch != 5 || pk.Yaw != 10 {
		t.Fatalf("look action did not release server rotation: pitch %f yaw %f", pk.Pitch, pk.Yaw)
	}
}

func TestJumpActionEmitsPressHoldAndRelease(t *testing.T) {
	ctx := context.Background()
	state := newPlayerState(mgl32.Vec3{}, 0, 0)
	jump := NewJumpAction(state, 2)
	if err := jump.Start(ctx); err != nil {
		t.Fatal(err)
	}

	if err := jump.Tick(ctx, action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	first := authInputPacket(state, 1)
	if !first.InputData.Load(packet.InputFlagStartJumping) || !first.InputData.Load(packet.InputFlagJumpPressedRaw) || !first.InputData.Load(packet.InputFlagJumping) {
		t.Fatalf("first jump packet missing press flags: %+v", first.InputData)
	}

	if err := jump.Tick(ctx, action.TickContext{Tick: 2}); err != nil {
		t.Fatal(err)
	}
	second := authInputPacket(state, 2)
	if second.InputData.Load(packet.InputFlagStartJumping) || !second.InputData.Load(packet.InputFlagJumpCurrentRaw) {
		t.Fatalf("second jump packet has wrong hold flags: %+v", second.InputData)
	}

	if err := jump.Tick(ctx, action.TickContext{Tick: 3}); err != nil {
		t.Fatal(err)
	}
	release := authInputPacket(state, 3)
	if !release.InputData.Load(packet.InputFlagJumpReleasedRaw) || release.InputData.Load(packet.InputFlagJumping) {
		t.Fatalf("jump release packet has wrong flags: %+v", release.InputData)
	}
	if !jump.Done() {
		t.Fatal("jump action should be done after release")
	}
}

func TestScenarioActionsPreserveV01Semantics(t *testing.T) {
	ctx := context.Background()
	idleState := newPlayerState(mgl32.Vec3{1, 64, 2}, 0, 0)
	idle := newScenarioAction(ScenarioIdle, idleState, 0)
	if err := idle.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := idle.Tick(ctx, action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	if pk := authInputPacket(idleState, 1); pk.MoveVector != (mgl32.Vec2{}) || pk.Delta != (mgl32.Vec3{}) {
		t.Fatalf("idle action unexpectedly moved: %+v", pk)
	}

	walkState := newPlayerState(mgl32.Vec3{1, 64, 2}, 0, 0)
	walk := newScenarioAction(ScenarioChunkWalk, walkState, 0)
	if err := walk.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := walk.Tick(ctx, action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	pk := authInputPacket(walkState, 1)
	if pk.MoveVector != (mgl32.Vec2{0, 1}) || !pk.InputData.Load(packet.InputFlagUp) {
		t.Fatalf("chunk-walk action lost forward input: %+v", pk)
	}
	if math.Abs(float64(pk.Delta[2]-chunkWalkStepPerTick)) > 1e-5 {
		t.Fatalf("chunk-walk action delta = %v", pk.Delta)
	}
}
