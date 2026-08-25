package bot

import (
	"context"
	"math"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	chunkFlyStepPerTick         = float32(0.32)
	chunkFlyVerticalStepPerTick = float32(0.40)
	chunkFlyMinimumAltitude     = float32(128)
	chunkFlyAltitudeGain        = float32(64)
	flightRequestRetryTicks     = uint64(40)
)

// FlyAction requests creative flight from the server, climbs to a safe altitude,
// then continuously traverses horizontally. Horizontal prediction does not begin
// until UpdateAbilities confirms that BDS accepted the flying state.
type FlyAction struct {
	state       *playerState
	writer      packetWriter
	vector      mgl32.Vec2
	stepPerTick float32
	yaw         float32
	targetY     float32
	elapsed     uint64
}

func NewChunkFlyAction(state *playerState, writer packetWriter, yaw float32) *FlyAction {
	position, _, _ := state.telemetrySnapshot()
	targetY := float32(math.Max(float64(chunkFlyMinimumAltitude), float64(position[1]+chunkFlyAltitudeGain)))
	return &FlyAction{
		state:       state,
		writer:      writer,
		vector:      mgl32.Vec2{0, 1},
		stepPerTick: chunkFlyStepPerTick,
		yaw:         yaw,
		targetY:     targetY,
	}
}

func (a *FlyAction) Name() string { return "chunk-fly" }

func (a *FlyAction) Start(context.Context) error {
	if a.writer == nil {
		return ErrPacketWriterUnavailable
	}
	a.state.setFlightControl(a.vector, a.stepPerTick, a.yaw, a.targetY, chunkFlyVerticalStepPerTick)
	return a.requestFlight()
}

func (a *FlyAction) Tick(context.Context, action.TickContext) error {
	a.state.setFlightControl(a.vector, a.stepPerTick, a.yaw, a.targetY, chunkFlyVerticalStepPerTick)
	a.elapsed++
	if !a.state.flightConfirmed() && a.elapsed%flightRequestRetryTicks == 0 {
		return a.requestFlight()
	}
	return nil
}

func (a *FlyAction) Done() bool { return false }

func (a *FlyAction) requestFlight() error {
	return a.writer.WritePacket(&packet.RequestAbility{
		Ability: packet.AbilityFlying,
		Value:   true,
	})
}

func (s *playerState) setFlightControl(vector mgl32.Vec2, stepPerTick, yaw, targetY, verticalStep float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.moveVector = vector
	s.control.moveStep = stepPerTick
	s.control.overrideYaw = true
	s.control.yaw = yaw
	s.control.fly = true
	s.control.flightTargetY = targetY
	s.control.verticalStep = verticalStep
	s.control.jump = false
}

func (s *playerState) setFlyingConfirmed(flying bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.flyingConfirmed != flying
	s.flyingConfirmed = flying
	return changed
}

func (s *playerState) flightConfirmed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flyingConfirmed
}

func (s *playerState) telemetrySnapshot() (mgl32.Vec3, bool, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.position, s.flyingConfirmed, s.serverCorrections
}
