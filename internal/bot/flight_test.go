package bot

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	"github.com/ReallocAll/bds-test-bot/internal/action"
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
		t.Fatal("flight transition input must request StartFlying")
	}
	if !input.InputData.Load(packet.InputFlagBlockBreakingDelayEnabled) {
		t.Fatal("auth input must include the normal Bedrock block-breaking-delay baseline flag")
	}
	if input.InputMode != packet.InputModeTouch || input.PlayMode != packet.PlayModeNormal || input.InteractionModel != packet.InteractionModelTouch {
		t.Fatalf("BDS input tuple = mode %d play %d interaction %d", input.InputMode, input.PlayMode, input.InteractionModel)
	}
	if input.MoveVector != (mgl32.Vec2{}) || input.Delta != (mgl32.Vec3{}) {
		t.Fatalf("movement predicted before server flight acknowledgement: %+v", input)
	}
	steady := authInputPacket(state, 2)
	if steady.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("StartFlying must be edge-triggered rather than asserted every input frame")
	}
}

func TestChunkFlyRetriesFlightTransitionWithAbilityRequest(t *testing.T) {
	ctx := context.Background()
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_ = authInputPacket(state, 0)
	for i := uint64(1); i < flightRequestRetryTicks; i++ {
		if err := fly.Tick(ctx, action.TickContext{Tick: i}); err != nil {
			t.Fatal(err)
		}
		if authInputPacket(state, i).InputData.Load(packet.InputFlagStartFlying) {
			t.Fatalf("unexpected StartFlying retry at tick %d", i)
		}
	}
	if err := fly.Tick(ctx, action.TickContext{Tick: flightRequestRetryTicks}); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 2 {
		t.Fatalf("flight request packets = %d, want initial + one retry", len(writer.packets))
	}
	retry := authInputPacket(state, flightRequestRetryTicks)
	if !retry.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("ability retry must queue one matching StartFlying transition")
	}
	if authInputPacket(state, flightRequestRetryTicks+1).InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("flight retry transition leaked into the following input frame")
	}
}

func TestChunkFlySeedsPublisherWithoutSyntheticTeleportAck(t *testing.T) {
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
	if !blocked.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("first publisher-waiting frame must carry the queued flight transition")
	}
	if blocked.MoveVector != (mgl32.Vec2{}) || blocked.Delta != (mgl32.Vec3{}) {
		t.Fatalf("placeholder position generated movement before publisher evidence: %+v", blocked)
	}

	publisherPosition := mgl32.Vec3{12.5, 68 + playerEyeHeight, -7.5}
	if !state.acceptPublisherPosition(publisherPosition) {
		t.Fatal("stable server publisher position was not accepted")
	}
	input := authInputPacket(state, 2)
	if input.InputData.Load(packet.InputFlagHandledTeleport) {
		t.Fatalf("chunk publisher must not synthesize HandledTeleport: %+v", input.InputData)
	}
	if input.InputData.Load(packet.InputFlagStartFlying) {
		t.Fatal("StartFlying must not remain asserted after its transition frame")
	}
	if input.MoveVector != (mgl32.Vec2{}) || input.Delta[1] <= 0 || input.Delta[0] != 0 || input.Delta[2] != 0 {
		t.Fatalf("publisher-seeded ascent prediction = move %v delta %v", input.MoveVector, input.Delta)
	}
	if !input.InputData.Load(packet.InputFlagAscend) || !input.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("publisher-seeded flight did not request ascent: %+v", input.InputData)
	}
	if input.Position[1] <= publisherPosition[1] {
		t.Fatalf("publisher-seeded ascent did not increase altitude: %v", input.Position)
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
	if first.Delta[1] <= 0 || first.Delta[0] != 0 || first.Delta[2] != 0 {
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

func TestChunkFlyClimbsBeforeHorizontalTraversal(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)

	ascent := authInputPacket(state, 1)
	if !ascent.InputData.Load(packet.InputFlagAscend) || !ascent.InputData.Load(packet.InputFlagWantUp) {
		t.Fatalf("chunk-fly ascent flags missing: %+v", ascent.InputData)
	}
	if ascent.MoveVector != (mgl32.Vec2{}) || ascent.Delta[1] <= 0 || ascent.Delta[0] != 0 || ascent.Delta[2] != 0 {
		t.Fatalf("chunk-fly ascent = move %v delta %v", ascent.MoveVector, ascent.Delta)
	}

	state.correct(mgl32.Vec3{0, fly.targetY, 0}, 0, 0, 0)
	cruise := authInputPacket(state, 2)
	if cruise.InputData.Load(packet.InputFlagAscend) || cruise.InputData.Load(packet.InputFlagWantUp) || cruise.InputData.Load(packet.InputFlagDescend) {
		t.Fatalf("cruise retained vertical flight flags: %+v", cruise.InputData)
	}
	if cruise.MoveVector != (mgl32.Vec2{0, 1}) || cruise.Delta[2] <= 0 || cruise.Delta[1] != 0 {
		t.Fatalf("chunk-fly cruise = move %v delta %v", cruise.MoveVector, cruise.Delta)
	}
	if cruise.AnalogueMoveVector != (mgl32.Vec2{}) {
		t.Fatalf("reference BDS tuple must leave analogue move vector unset: %v", cruise.AnalogueMoveVector)
	}
	if cruise.RawMoveVector[0] != 0 || math.Abs(float64(cruise.RawMoveVector[1]-chunkFlyStepPerTick)) > 1e-6 {
		t.Fatalf("raw move vector = %v, want local per-tick displacement %f", cruise.RawMoveVector, chunkFlyStepPerTick)
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
	corrected := mgl32.Vec3{10, fly.targetY, 20}
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

func TestChunkFlyDescendsAfterAuthoritativeOvershoot(t *testing.T) {
	state := newPlayerState(mgl32.Vec3{0, 64, 0}, 0, 0)
	writer := &recordingPacketWriter{}
	fly := NewChunkFlyAction(state, writer, 0)
	if err := fly.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.setFlyingConfirmed(true)
	state.correct(mgl32.Vec3{0, fly.targetY + 4, 0}, 0, 0, 0)

	input := authInputPacket(state, 1)
	if !input.InputData.Load(packet.InputFlagDescend) || !input.InputData.Load(packet.InputFlagWantDown) {
		t.Fatalf("overshoot did not request descent: %+v", input.InputData)
	}
	if input.MoveVector != (mgl32.Vec2{}) || input.Delta[1] >= 0 || input.Delta[0] != 0 || input.Delta[2] != 0 {
		t.Fatalf("overshoot descent = move %v delta %v", input.MoveVector, input.Delta)
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
