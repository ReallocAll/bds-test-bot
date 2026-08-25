package bot

import (
	"context"
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
	if input.InputData.Load(packet.InputFlagBlockBreakingDelayEnabled) {
		t.Fatal("flight heartbeat must not include the block-breaking-delay baseline flag")
	}
	if input.InputMode != packet.InputModeMouse || input.PlayMode != packet.PlayModeScreen || input.InteractionModel != packet.InteractionModelCrosshair {
		t.Fatalf("desktop input tuple = mode %d play %d interaction %d", input.InputMode, input.PlayMode, input.InteractionModel)
	}
	if input.MoveVector != (mgl32.Vec2{}) || input.Delta != (mgl32.Vec3{}) {
		t.Fatalf("movement predicted before server flight acknowledgement: %+v", input)
	}
}

func TestChunkFlyAcknowledgesPublisherSpawnBeforeMovement(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{12.5, 32769.625, -7.5}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if state.positionReadySnapshot() {
		t.Fatal("placeholder StartGame altitude must not be position-ready")
	}
	if fly.targetY != chunkFlyMinimumAltitude {
		t.Fatalf("placeholder target altitude = %f, want %f", fly.targetY, chunkFlyMinimumAltitude)
	}
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)
	blocked := authInputPacket(state, 1)
	if blocked.MoveVector != (mgl32.Vec2{}) || blocked.Delta != (mgl32.Vec3{}) {
		t.Fatalf("placeholder position generated movement before publisher evidence: %+v", blocked)
	}

	publisherPosition := mgl32.Vec3{12.5, 68 + playerEyeHeight, -7.5}
	if !state.acceptPublisherPosition(publisherPosition) {
		t.Fatal("stable server publisher position was not accepted")
	}
	if !state.positionReadySnapshot() {
		t.Fatal("publisher position did not make movement position-ready")
	}
	ack := authInputPacket(state, 2)
	if !ack.InputData.Load(packet.InputFlagHandledTeleport) {
		t.Fatalf("publisher spawn acknowledgement missing HandledTeleport: %+v", ack.InputData)
	}
	if !ack.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("publisher spawn acknowledgement must keep flying asserted")
	}
	if ack.Position != publisherPosition || ack.MoveVector != (mgl32.Vec2{}) || ack.Delta != (mgl32.Vec3{}) {
		t.Fatalf("publisher spawn acknowledgement must be stationary at authoritative position: %+v", ack)
	}

	climb := authInputPacket(state, 3)
	if climb.InputData.Load(packet.InputFlagHandledTeleport) {
		t.Fatal("HandledTeleport must only be sent for the acknowledgement frame")
	}
	if !climb.InputData.Load(packet.InputFlagAscend) || !climb.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("post-ack climb missing ascent flags: %+v", climb.InputData)
	}
	if climb.Delta != (mgl32.Vec3{}) || climb.MoveVector != (mgl32.Vec2{}) {
		t.Fatalf("post-ack climb must be server-driven with zero client delta: %+v", climb)
	}
	if state.acceptPublisherPosition(mgl32.Vec3{12.5, fly.targetY, -7.5}) {
		t.Fatal("publisher seeding must not overwrite an already-ready predicted position")
	}
}

func TestPublisherEyePositionUsesAuthoritativeBlockPosition(t *testing.T) {
	got := publisherEyePosition(protocol.BlockPos{266, 70, 159})
	want := mgl32.Vec3{266.5, 70 + playerEyeHeight, 159.5}
	if got != want {
		t.Fatalf("publisher eye position = %v, want %v", got, want)
	}
}

func TestPublisherObservationDrivesAuthoritativeCruisePosition(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{4.5, 80, 8.5}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)
	serverPos := mgl32.Vec3{7.5, fly.targetY, 11.5}
	if !state.observePublisherPosition(serverPos) {
		t.Fatal("publisher position was not observed")
	}
	input := authInputPacket(state, 0)
	if input.Position != serverPos {
		t.Fatalf("auth input position = %v, want server position %v", input.Position, serverPos)
	}
	if input.MoveVector != (mgl32.Vec2{0, 1}) || input.Delta != (mgl32.Vec3{}) {
		t.Fatalf("server-driven cruise = move %v delta %v", input.MoveVector, input.Delta)
	}
	if input.Tick != 0 || input.RawMoveVector != (mgl32.Vec2{}) || input.AnalogueMoveVector != (mgl32.Vec2{}) {
		t.Fatalf("flight heartbeat must keep tick/raw/analogue neutral: %+v", input)
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
	if climb.Delta != (mgl32.Vec3{}) || climb.MoveVector != (mgl32.Vec2{}) {
		t.Fatalf("climb packet must carry ascent intent without client delta: delta %v move %v", climb.Delta, climb.MoveVector)
	}

	if !state.observePublisherPosition(mgl32.Vec3{0, fly.targetY, 0}) {
		t.Fatal("server publisher update did not advance flight state")
	}
	cruise := authInputPacket(state, 2)
	if cruise.MoveVector != (mgl32.Vec2{0, 1}) {
		t.Fatalf("cruise move vector = %v", cruise.MoveVector)
	}
	if cruise.Delta != (mgl32.Vec3{}) {
		t.Fatalf("server-driven cruise must not fabricate client delta: %v", cruise.Delta)
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
	if input.Delta != (mgl32.Vec3{}) || input.MoveVector != (mgl32.Vec2{}) ||
		!input.InputData.Load(packet.InputFlagAscend) || !input.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("corrected flight should recover altitude through server-driven ascent intent: %+v", input)
	}
	_, _, corrections := state.telemetrySnapshot()
	if corrections != 1 {
		t.Fatalf("server corrections = %d, want 1", corrections)
	}
}

func TestServerTickSyncDrivesNextAuthInputTick(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{}, 0, 0)
	if got := state.nextInputTick(); got != 0 {
		t.Fatalf("initial input tick = %d, want 0", got)
	}
	if got := state.nextInputTick(); got != 1 {
		t.Fatalf("second input tick = %d, want 1", got)
	}
	state.syncServerTick(240)
	next, synced := state.tickSnapshot()
	if !synced || next != 241 {
		t.Fatalf("tick sync = next %d synced %v, want 241 true", next, synced)
	}
	if got := state.nextInputTick(); got != 241 {
		t.Fatalf("post-sync input tick = %d, want 241", got)
	}
	state.syncServerTick(200)
	if got := state.nextInputTick(); got != 242 {
		t.Fatalf("stale server tick moved input clock backwards: %d", got)
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
	flags := protocol.NewInputFlags(packet.InputFlagCount)
	flags.Set(packet.InputFlagAscend)
	flags.Set(packet.InputFlagWantUp)
	if err := writer.WritePacket(&packet.PlayerAuthInput{InputData: flags}); err != nil {
		t.Fatal(err)
	}
	if auth.Load() != 1 || movement.Load() != 1 || actions.Load() != 0 {
		t.Fatalf("counters auth=%d movement=%d actions=%d", auth.Load(), movement.Load(), actions.Load())
	}
}
