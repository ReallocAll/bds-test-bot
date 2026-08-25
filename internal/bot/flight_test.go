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
	if !input.InputData.Load(packet.InputFlagBlockBreakingDelayEnabled) {
		t.Fatal("auth input must include the normal Bedrock block-breaking-delay baseline flag")
	}
	if input.InputMode != packet.InputModeMouse || input.PlayMode != packet.PlayModeScreen || input.InteractionModel != packet.InteractionModelCrosshair {
		t.Fatalf("desktop input tuple = mode %d play %d interaction %d", input.InputMode, input.PlayMode, input.InteractionModel)
	}
	if input.MoveVector != (mgl32.Vec2{}) || input.Delta != (mgl32.Vec3{}) {
		t.Fatalf("movement predicted before server flight acknowledgement: %+v", input)
	}
}

func TestChunkFlyAcknowledgesPublisherSpawnBeforeHorizontalPrediction(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{12.5, 32769.625, -7.5}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if state.positionReadySnapshot() {
		t.Fatal("placeholder StartGame altitude must not be position-ready")
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
	ack := authInputPacket(state, 2)
	if !ack.InputData.Load(packet.InputFlagHandledTeleport) || !ack.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatalf("publisher spawn acknowledgement flags = %+v", ack.InputData)
	}
	if ack.Position != publisherPosition || ack.MoveVector != (mgl32.Vec2{}) || ack.Delta != (mgl32.Vec3{}) {
		t.Fatalf("publisher spawn acknowledgement must be stationary: %+v", ack)
	}

	cruise := authInputPacket(state, 3)
	if cruise.InputData.Load(packet.InputFlagHandledTeleport) {
		t.Fatal("HandledTeleport must only be sent for the acknowledgement frame")
	}
	if cruise.InputData.Load(packet.InputFlagAscend) || cruise.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("horizontal-only diagnostic unexpectedly requested ascent: %+v", cruise.InputData)
	}
	if cruise.MoveVector != (mgl32.Vec2{0, 1}) || cruise.Delta[2] <= 0 || cruise.Delta[1] != 0 {
		t.Fatalf("horizontal prediction = move %v delta %v", cruise.MoveVector, cruise.Delta)
	}
	if cruise.Position[1] != publisherPosition[1] {
		t.Fatalf("horizontal diagnostic changed altitude: %v", cruise.Position)
	}
}

func TestPublisherEyePositionUsesAuthoritativeBlockPosition(t *testing.T) {
	got := publisherEyePosition(protocol.BlockPos{266, 70, 159})
	want := mgl32.Vec3{266.5, 70 + playerEyeHeight, 159.5}
	if got != want {
		t.Fatalf("publisher eye position = %v, want %v", got, want)
	}
}

func TestPublisherTelemetryDoesNotOverwritePredictedFlightPosition(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{4.5, 80, 8.5}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)
	first := authInputPacket(state, 7)
	if first.Delta[2] <= 0 {
		t.Fatalf("first predicted flight delta = %v", first.Delta)
	}
	before, _, _ := state.telemetrySnapshot()
	// NetworkChunkPublisherUpdate remains server evidence only after the initial
	// spawn seed. The bot read loop no longer calls observePublisherPosition here.
	after, _, _ := state.telemetrySnapshot()
	if after != before {
		t.Fatalf("publisher telemetry overwrote prediction: before=%v after=%v", before, after)
	}
}

func TestChunkFlyHorizontalDiagnosticKeepsAuthoritativeAltitude(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)

	input := authInputPacket(state, 1)
	if input.InputData.Load(packet.InputFlagAscend) || input.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("horizontal diagnostic requested ascent: %+v", input.InputData)
	}
	if input.MoveVector != (mgl32.Vec2{0, 1}) || input.Delta[2] <= 0 || input.Delta[1] != 0 {
		t.Fatalf("horizontal diagnostic = move %v delta %v", input.MoveVector, input.Delta)
	}
	if input.Position[1] != 64 {
		t.Fatalf("horizontal diagnostic altitude = %f, want 64", input.Position[1])
	}
	if input.RawMoveVector != input.MoveVector || input.AnalogueMoveVector != input.MoveVector {
		t.Fatalf("flight packet missing normal raw/analogue movement fields: %+v", input)
	}
}

func TestChunkFlyHorizontalPredictionResumesFromServerCorrection(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 90)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)
	corrected := mgl32.Vec3{10, 90, 20}
	state.correct(corrected, 0, 90, 90)

	input := authInputPacket(state, 1)
	if input.Delta[0] >= 0 || input.Delta[1] != 0 {
		t.Fatalf("corrected horizontal flight delta = %v", input.Delta)
	}
	if input.Position[1] != corrected[1] || input.Position[0] >= corrected[0] {
		t.Fatalf("flight did not resume horizontally from correction: %v", input.Position)
	}
	if input.InputData.Load(packet.InputFlagAscend) || input.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("horizontal correction recovery requested ascent: %+v", input.InputData)
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
