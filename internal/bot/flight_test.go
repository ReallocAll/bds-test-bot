package bot

import (
	"context"
	"math"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestChunkFlyRequestsServerAbilityBeforePredictingMovement(t *testing.T) {
	ctx := context.Background()
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 1 {
		t.Fatalf("flight request packets = %d, want 1", len(writer.packets))
	}
	request, ok := writer.packets[0].(*packet.RequestAbility)
	if !ok {
		t.Fatalf("flight request packet type = %T", writer.packets[0])
	}
	if request.Ability != packet.AbilityFlying {
		t.Fatalf("requested ability = %d, want flying", request.Ability)
	}
	enabled, ok := request.Value.(bool)
	if !ok || !enabled {
		t.Fatalf("flight ability value = %#v, want true", request.Value)
	}

	input := authInputPacket(state, 1)
	if !input.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("unacknowledged flight input must request StartFlying")
	}
	if input.MoveVector != (mgl32.Vec2{}) || input.Delta != (mgl32.Vec3{}) {
		t.Fatalf("movement predicted before server flight acknowledgement: %+v", input)
	}
}

func TestChunkFlyResetsOutOfRangeSpawnAltitude(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{12, 32769.625, -8}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if !fly.resetAltitude {
		t.Fatal("out-of-range spawn altitude must request a server-authoritative reset")
	}
	if fly.targetY != chunkFlyMaximumAltitude {
		t.Fatalf("target altitude = %f, want %f", fly.targetY, chunkFlyMaximumAltitude)
	}
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 2 {
		t.Fatalf("startup packets = %d, want teleport + flight request", len(writer.packets))
	}
	command, ok := writer.packets[0].(*packet.CommandRequest)
	if !ok {
		t.Fatalf("altitude reset packet type = %T", writer.packets[0])
	}
	if !strings.Contains(command.CommandLine, "tp @s ~ 256 ~") {
		t.Fatalf("altitude reset command = %q", command.CommandLine)
	}
	if command.CommandOrigin.Origin != protocol.CommandOriginPlayer || command.Version != "latest" {
		t.Fatalf("unexpected altitude reset command metadata: %+v", command)
	}
	if request, ok := writer.packets[1].(*packet.RequestAbility); !ok || request.Ability != packet.AbilityFlying {
		t.Fatalf("second startup packet = %#v, want flying request", writer.packets[1])
	}
}

func TestChunkFlyClimbsThenTraversesAtSafeAltitude(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)

	climb := authInputPacket(state, 1)
	if !climb.InputData.Load(packet.InputFlagAscend) || !climb.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("climb packet missing flight ascent flags: %+v", climb.InputData)
	}
	if climb.Delta[1] <= 0 || climb.MoveVector != (mgl32.Vec2{}) {
		t.Fatalf("climb packet = delta %v move %v", climb.Delta, climb.MoveVector)
	}

	state.update(mgl32.Vec3{0, fly.targetY, 0}, 0, 0, 0, false)
	cruise := authInputPacket(state, 2)
	if cruise.MoveVector != (mgl32.Vec2{0, 1}) {
		t.Fatalf("cruise move vector = %v", cruise.MoveVector)
	}
	if math.Abs(float64(cruise.Delta[2]-chunkFlyStepPerTick)) > 1e-5 || math.Abs(float64(cruise.Delta[1])) > 1e-5 {
		t.Fatalf("cruise delta = %v", cruise.Delta)
	}
}

func TestChunkFlyResumesFromServerCorrection(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 90)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)
	state.correct(mgl32.Vec3{10, fly.targetY - 1, 20}, 0, 90, 90)

	input := authInputPacket(state, 1)
	if input.Position[0] != 10 || input.Position[2] != 20 {
		t.Fatalf("flight did not resume from corrected horizontal position: %v", input.Position)
	}
	if input.Delta[1] <= 0 || input.MoveVector != (mgl32.Vec2{}) {
		t.Fatalf("corrected flight should recover altitude before horizontal traversal: %+v", input)
	}
	_, _, corrections := state.telemetrySnapshot()
	if corrections != 1 {
		t.Fatalf("server corrections = %d, want 1", corrections)
	}
}

func TestFlightAbilityStateUsesServerValues(t *testing.T) {
	mayFly, flying := flightAbilityState(protocol.AbilityData{Layers: []protocol.AbilityLayer{{
		Abilities: protocol.AbilityMayFly | protocol.AbilityFlying,
		Values:    protocol.AbilityMayFly | protocol.AbilityFlying,
	}}})
	if !mayFly || !flying {
		t.Fatalf("server ability state = mayFly %v flying %v", mayFly, flying)
	}

	mayFly, flying = flightAbilityState(protocol.AbilityData{Layers: []protocol.AbilityLayer{{
		Abilities: protocol.AbilityMayFly | protocol.AbilityFlying,
		Values:    protocol.AbilityMayFly,
	}}})
	if !mayFly || flying {
		t.Fatalf("disabled flying value misread: mayFly %v flying %v", mayFly, flying)
	}
}

func TestChunkFlyScenarioFactoryUsesFleetHeading(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Scenario = ScenarioChunkFly
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	built, err := newScenarioAction(cfg, state, 90, writer, "TestBot", 1)
	if err != nil {
		t.Fatal(err)
	}
	fly, ok := built.(*FlyAction)
	if !ok {
		t.Fatalf("chunk-fly factory action = %T", built)
	}
	if fly.yaw != 90 {
		t.Fatalf("chunk-fly heading = %f, want 90", fly.yaw)
	}
}

func TestAuthInputWriterCountsVerticalFlightMovement(t *testing.T) {
	var auth atomic.Uint64
	var movement atomic.Uint64
	var actions atomic.Uint64
	underlying := &recordingPacketWriter{}
	writer := authInputWriter{
		writer:        underlying,
		authCount:     &auth,
		movementCount: &movement,
		actionCount:   &actions,
	}
	if err := writer.WritePacket(&packet.PlayerAuthInput{Delta: mgl32.Vec3{0, 0.4, 0}}); err != nil {
		t.Fatal(err)
	}
	if auth.Load() != 1 || movement.Load() != 1 || actions.Load() != 0 {
		t.Fatalf("counters auth=%d movement=%d actions=%d", auth.Load(), movement.Load(), actions.Load())
	}
}
