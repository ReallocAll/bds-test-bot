package bot

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type countingWriter struct {
	count atomic.Int32
}

func (w *countingWriter) WritePacket(packet.Packet) error {
	w.count.Add(1)
	return nil
}

func TestAuthInputUsesPresentEmptyFlags(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{1, 64, 2}, 0, 90)
	pk := authInputPacket(state, 7)
	if !pk.InputData.Present() {
		t.Fatal("InputData must be present even when no input flags are set")
	}
	if pk.Tick != 7 || pk.Position != (mgl32.Vec3{1, 64, 2}) {
		t.Fatalf("unexpected auth input: %+v", pk)
	}
	if pk.MoveVector != (mgl32.Vec2{}) || pk.Delta != (mgl32.Vec3{}) {
		t.Fatalf("idle input unexpectedly moved: %+v", pk)
	}
}

func TestAuthInputAcknowledgesTeleportOnce(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{}, 0, 0)
	state.update(mgl32.Vec3{4, 70, 5}, 1, 2, 2, true)
	first := authInputPacket(state, 1)
	second := authInputPacket(state, 2)
	if !first.InputData.Load(packet.InputFlagHandledTeleport) {
		t.Fatal("first packet must acknowledge teleport")
	}
	if second.InputData.Load(packet.InputFlagHandledTeleport) {
		t.Fatal("teleport acknowledgement must be one-shot")
	}
}

func TestChunkWalkFansFleetHeadings(t *testing.T) {
	if got := scenarioHeading(0, 1, 4); math.Abs(float64(got)) > 1e-5 {
		t.Fatalf("first heading = %f, want 0", got)
	}
	if got := scenarioHeading(0, 2, 4); math.Abs(float64(got-90)) > 1e-5 {
		t.Fatalf("second heading = %f, want 90", got)
	}
	if got := scenarioHeading(0, 3, 4); math.Abs(math.Abs(float64(got))-180) > 1e-5 {
		t.Fatalf("third heading = %f, want +/-180", got)
	}
}

func TestTickLoopStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &countingWriter{}
	state := newPlayerState(mgl32.Vec3{}, 0, 0)
	done := make(chan error, 1)
	cfg := DefaultConfig()
	cfg.Scenario = ScenarioIdle
	go func() { done <- runTickLoop(ctx, writer, state, cfg, 0, "TestBot", 1) }()
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("tick loop did not stop after cancellation")
	}
	if writer.count.Load() == 0 {
		t.Fatal("tick loop did not send any input packets")
	}
}

func TestChunkWalkTickLoopCancelsDuringSpawnSettle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &countingWriter{}
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	done := make(chan error, 1)
	cfg := DefaultConfig()
	cfg.Scenario = ScenarioChunkWalk
	go func() { done <- runTickLoop(ctx, writer, state, cfg, 0, "TestBot", 1) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("chunk-walk tick loop did not cancel during spawn settle window")
	}
	if writer.count.Load() != 0 {
		t.Fatalf("sent %d packets before chunk-walk movement settle completed", writer.count.Load())
	}
}

func TestPublisherCannotSeedAfterAuthInputStarts(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0.5, 32769.62, 0.5}, 0, 0)
	if state.positionReadySnapshot() {
		t.Fatal("placeholder StartGame position unexpectedly ready")
	}
	if tick := state.nextInputTick(); tick != 0 {
		t.Fatalf("bootstrap tick = %d, want 0", tick)
	}
	if state.acceptPublisherPosition(mgl32.Vec3{0.5, 88.62, 0.5}) {
		t.Fatal("publisher seeded prediction after auth input stream started")
	}
	if state.positionReadySnapshot() {
		t.Fatal("publisher made correction-bootstrap state ready")
	}
	state.correct(mgl32.Vec3{0.5, 88.62, 0.5}, 0, 0, 0)
	if !state.positionReadySnapshot() {
		t.Fatal("server correction did not establish authoritative position")
	}
}

func TestChunkFlyCorrectionProbeIsOneShotAndNonAccumulating(t *testing.T) {
	start := mgl32.Vec3{0.5, 32769.62, 0.5}
	state := newPlayerState(start, 0, 0)
	state.setFlightControl(mgl32.Vec2{0, 1}, chunkFlyStepPerTick, 0, chunkFlyMinimumAltitude, chunkFlyVerticalStepPerTick)
	state.setFlyingConfirmed(true)
	state.nextInputTick()

	probe := authInputPacket(state, 1)
	if probe.MoveVector != (mgl32.Vec2{0, 1}) || probe.Delta[2] <= 0 || probe.Delta[0] != 0 || probe.Delta[1] != 0 {
		t.Fatalf("correction probe = move %v delta %v", probe.MoveVector, probe.Delta)
	}
	if probe.Position[2] <= start[2] || probe.RawMoveVector[1] <= 0 {
		t.Fatalf("correction probe did not encode post-move position/raw input: position=%v raw=%v", probe.Position, probe.RawMoveVector)
	}
	position, _, _ := state.telemetrySnapshot()
	if position != start {
		t.Fatalf("probe permanently advanced speculative state: got %v want %v", position, start)
	}

	idle := authInputPacket(state, 2)
	if idle.MoveVector != (mgl32.Vec2{}) || idle.Delta != (mgl32.Vec3{}) || idle.RawMoveVector != (mgl32.Vec2{}) || idle.Position != start {
		t.Fatalf("second pre-correction frame must be idle: %+v", idle)
	}
}

func TestChunkFlyCorrectionProbeHandsOffToSafeAscent(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0.5, 32769.62, 0.5}, 0, 0)
	state.setFlightControl(mgl32.Vec2{0, 1}, chunkFlyStepPerTick, 0, chunkFlyMinimumAltitude, chunkFlyVerticalStepPerTick)
	state.setFlyingConfirmed(true)
	state.nextInputTick()
	_ = authInputPacket(state, 1)

	corrected := mgl32.Vec3{0.5, 70.62, 0.5}
	state.correct(corrected, 0, 0, 0)
	ascent := authInputPacket(state, 2)
	if !ascent.InputData.Load(packet.InputFlagAscend) || !ascent.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("corrected flight did not start safe ascent: %+v", ascent.InputData)
	}
	if ascent.Delta[1] <= 0 || ascent.Position[1] <= corrected[1] {
		t.Fatalf("safe ascent did not advance from correction: position=%v delta=%v", ascent.Position, ascent.Delta)
	}
}

func TestCorrectionReplaysPredictionsAfterCorrectedTick(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{10, 70, 20}, 0, 0)
	state.setMoveControl(mgl32.Vec2{0, 1}, chunkWalkStepPerTick, 0)
	_ = authInputPacket(state, 40)
	_ = authInputPacket(state, 41)
	_ = authInputPacket(state, 42)

	corrected := mgl32.Vec3{10, 70, 20.25}
	state.correctPrediction(corrected, 0, 0, 0, 40)
	position, _, corrections := state.telemetrySnapshot()
	wantZ := corrected[2] + 2*chunkWalkStepPerTick
	if math.Abs(float64(position[2]-wantZ)) > 1e-5 || position[0] != corrected[0] || position[1] != corrected[1] {
		t.Fatalf("replayed position = %v, want x=%f y=%f z=%f", position, corrected[0], corrected[1], wantZ)
	}
	if corrections != 1 {
		t.Fatalf("server corrections = %d, want 1", corrections)
	}
}

func TestCorrectionReplayDoesNotCommitGroundGravityDelta(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	state.setMoveControl(mgl32.Vec2{0, 1}, chunkWalkStepPerTick, 0)
	pk := authInputPacket(state, 2)
	if pk.Delta[1] >= 0 {
		t.Fatalf("walk packet should carry ground gravity delta: %v", pk.Delta)
	}
	state.correctPrediction(mgl32.Vec3{0, 64, 0}, 0, 0, 0, 1)
	position, _, _ := state.telemetrySnapshot()
	if position[1] != 64 {
		t.Fatalf("gravity delta was replayed into prediction position: %v", position)
	}
	if math.Abs(float64(position[2]-chunkWalkStepPerTick)) > 1e-5 {
		t.Fatalf("horizontal prediction was not replayed: %v", position)
	}
}
