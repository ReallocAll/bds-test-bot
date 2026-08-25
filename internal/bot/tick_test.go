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
