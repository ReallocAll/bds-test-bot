package bot

import (
	"context"
	"fmt"
	"math"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	chunkFlyStepPerTick         = float32(0.32)
	chunkFlyVerticalStepPerTick = float32(0.40)
	chunkFlyMinimumAltitude     = float32(128)
	chunkFlyMaximumAltitude     = float32(256)
	chunkFlyAltitudeGain        = float32(64)
	chunkFlyValidMinY           = float32(-64)
	chunkFlyValidMaxY           = float32(320)
	flightRequestRetryTicks     = uint64(40)
)

// FlyAction requests creative flight from the server, climbs to a safe altitude,
// then continuously traverses horizontally. Horizontal prediction does not begin
// until UpdateAbilities confirms that BDS accepted the flying state.
type FlyAction struct {
	state         *playerState
	writer        packetWriter
	vector        mgl32.Vec2
	stepPerTick   float32
	yaw           float32
	targetY       float32
	resetAltitude bool
	elapsed       uint64
}

func NewChunkFlyAction(state *playerState, writer packetWriter, yaw float32) *FlyAction {
	position, _, _ := state.telemetrySnapshot()
	targetY := position[1] + chunkFlyAltitudeGain
	if targetY < chunkFlyMinimumAltitude {
		targetY = chunkFlyMinimumAltitude
	}
	if targetY > chunkFlyMaximumAltitude {
		targetY = chunkFlyMaximumAltitude
	}
	return &FlyAction{
		state:         state,
		writer:        writer,
		vector:        mgl32.Vec2{0, 1},
		stepPerTick:   chunkFlyStepPerTick,
		yaw:           yaw,
		targetY:       targetY,
		resetAltitude: position[1] < chunkFlyValidMinY || position[1] > chunkFlyValidMaxY,
	}
}

func (a *FlyAction) Name() string { return "chunk-fly" }

func (a *FlyAction) Start(context.Context) error {
	if a.writer == nil {
		return ErrPacketWriterUnavailable
	}
	a.state.setFlightControl(a.vector, a.stepPerTick, a.yaw, a.targetY, chunkFlyVerticalStepPerTick)
	if a.resetAltitude {
		if err := a.requestAltitudeReset(); err != nil {
			return err
		}
		// StartGame may carry the Bedrock placeholder altitude around Y=32768
		// before the server has established the real player position. The
		// one-shot /tp above is the server-authoritative reset; immediately
		// rebasing the local prediction to the same requested Y prevents the
		// 20 TPS AuthInput stream from spending minutes descending from the
		// placeholder and racing the server's teleport result.
		a.state.rebaseAltitude(a.targetY)
	}
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

func (a *FlyAction) requestAltitudeReset() error {
	return a.writer.WritePacket(&packet.CommandRequest{
		CommandLine: fmt.Sprintf("/tp @s ~ %.0f ~", a.targetY),
		CommandOrigin: protocol.CommandOrigin{
			Origin:         protocol.CommandOriginPlayer,
			UUID:           uuid.New(),
			PlayerUniqueID: 0,
		},
		Internal: false,
		Version:  "latest",
	})
}

func (a *FlyAction) requestFlight() error {
	return a.writer.WritePacket(&packet.RequestAbility{
		Ability: packet.AbilityFlying,
		Value:   true,
	})
}

func flightAbilityState(data protocol.AbilityData) (mayFly, flying bool) {
	for _, layer := range data.Layers {
		if layer.Abilities&protocol.AbilityMayFly != 0 && layer.Values&protocol.AbilityMayFly != 0 {
			mayFly = true
		}
		if layer.Abilities&protocol.AbilityFlying != 0 && layer.Values&protocol.AbilityFlying != 0 {
			flying = true
		}
	}
	return mayFly, flying
}

func horizontalDistance(start, end mgl32.Vec3) float64 {
	return math.Hypot(float64(end[0]-start[0]), float64(end[2]-start[2]))
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

func (s *playerState) rebaseAltitude(y float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.position[1] = y
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
