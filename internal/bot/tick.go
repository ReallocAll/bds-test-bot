package bot

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const chunkWalkStepPerTick = float32(0.18)

type packetWriter interface {
	WritePacket(packet.Packet) error
}

type inputControl struct {
	moveVector    mgl32.Vec2
	moveStep      float32
	overrideYaw   bool
	yaw           float32
	overridePitch bool
	pitch         float32
	jump          bool
	lastJump      bool
}

type playerState struct {
	mu              sync.Mutex
	position        mgl32.Vec3
	pitch           float32
	yaw             float32
	headYaw         float32
	handledTeleport bool
	control         inputControl
}

func newPlayerState(position mgl32.Vec3, pitch, yaw float32) *playerState {
	return &playerState{position: position, pitch: pitch, yaw: yaw, headYaw: yaw}
}

func (s *playerState) update(position mgl32.Vec3, pitch, yaw, headYaw float32, teleport bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.position = position
	s.pitch = pitch
	s.yaw = yaw
	s.headYaw = headYaw
	s.handledTeleport = s.handledTeleport || teleport
}

func (s *playerState) setIdleControl() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.moveVector = mgl32.Vec2{}
	s.control.moveStep = 0
	s.control.overrideYaw = false
	s.control.overridePitch = false
	s.control.jump = false
}

func (s *playerState) setMoveControl(vector mgl32.Vec2, stepPerTick, yaw float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.moveVector = vector
	s.control.moveStep = stepPerTick
	s.control.overrideYaw = true
	s.control.yaw = yaw
}

func (s *playerState) clearMoveControl() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.moveVector = mgl32.Vec2{}
	s.control.moveStep = 0
	s.control.overrideYaw = false
}

func (s *playerState) setLookControl(pitch, yaw float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.overridePitch = true
	s.control.pitch = pitch
	s.control.overrideYaw = true
	s.control.yaw = yaw
}

func (s *playerState) clearLookControl() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.overridePitch = false
	s.control.overrideYaw = false
}

func (s *playerState) setJumpControl(jump bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.jump = jump
}

func (s *playerState) inputSnapshot() (mgl32.Vec3, float32, float32, float32, bool, mgl32.Vec3, mgl32.Vec2, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	teleport := s.handledTeleport
	s.handledTeleport = false
	pitch := s.pitch
	yaw := s.yaw
	headYaw := s.headYaw

	if s.control.overridePitch {
		pitch = s.control.pitch
	}
	if s.control.overrideYaw {
		yaw = s.control.yaw
		headYaw = s.control.yaw
	}

	moveVector := s.control.moveVector
	delta := mgl32.Vec3{}
	if teleport {
		moveVector = mgl32.Vec2{}
	} else if moveVector != (mgl32.Vec2{}) && s.control.moveStep != 0 {
		yawRad := float64(yaw) * math.Pi / 180
		forward := float64(moveVector[1])
		strafe := float64(moveVector[0])
		delta = mgl32.Vec3{
			float32(math.Cos(yawRad)*strafe-math.Sin(yawRad)*forward) * s.control.moveStep,
			0,
			float32(math.Sin(yawRad)*strafe+math.Cos(yawRad)*forward) * s.control.moveStep,
		}
		s.position[0] += delta[0]
		s.position[2] += delta[2]
	}

	jump := s.control.jump && !teleport
	lastJump := s.control.lastJump
	s.control.lastJump = jump

	return s.position, pitch, yaw, headYaw, teleport, delta, moveVector, jump, lastJump
}

func scenarioHeading(baseYaw float32, index, count int) float32 {
	if count <= 1 {
		return baseYaw
	}
	heading := float64(baseYaw) + 360*float64(index-1)/float64(count)
	heading = math.Mod(heading+180, 360)
	if heading < 0 {
		heading += 360
	}
	return float32(heading - 180)
}

func authInputPacket(s *playerState, tick uint64) *packet.PlayerAuthInput {
	position, pitch, yaw, headYaw, handledTeleport, delta, moveVector, jumping, wasJumping := s.inputSnapshot()
	flags := protocol.NewInputFlags(packet.InputFlagCount)
	if handledTeleport {
		flags.Set(packet.InputFlagHandledTeleport)
	}

	if moveVector[1] > 0 {
		flags.Set(packet.InputFlagUp)
	}
	if moveVector[1] < 0 {
		flags.Set(packet.InputFlagDown)
	}
	if moveVector[0] < 0 {
		flags.Set(packet.InputFlagLeft)
	}
	if moveVector[0] > 0 {
		flags.Set(packet.InputFlagRight)
	}

	if jumping {
		flags.Set(packet.InputFlagJumping)
		flags.Set(packet.InputFlagJumpCurrentRaw)
		if !wasJumping {
			flags.Set(packet.InputFlagJumpDown)
			flags.Set(packet.InputFlagStartJumping)
			flags.Set(packet.InputFlagJumpPressedRaw)
		}
	} else if wasJumping {
		flags.Set(packet.InputFlagJumpReleasedRaw)
	}

	pitchRad := float64(pitch) * math.Pi / 180
	yawRad := float64(yaw) * math.Pi / 180
	camera := mgl32.Vec3{
		-float32(math.Sin(yawRad) * math.Cos(pitchRad)),
		-float32(math.Sin(pitchRad)),
		float32(math.Cos(yawRad) * math.Cos(pitchRad)),
	}

	return &packet.PlayerAuthInput{
		Pitch:              pitch,
		Yaw:                yaw,
		Position:           position,
		MoveVector:         moveVector,
		HeadYaw:            headYaw,
		InputData:          flags,
		InputMode:          packet.InputModeMouse,
		PlayMode:           packet.PlayModeScreen,
		InteractionModel:   packet.InteractionModelCrosshair,
		InteractPitch:      pitch,
		InteractYaw:        yaw,
		Tick:               tick,
		Delta:              delta,
		AnalogueMoveVector: moveVector,
		CameraOrientation:  camera,
		RawMoveVector:      moveVector,
	}
}

func runTickLoop(ctx context.Context, writer packetWriter, state *playerState, cfg Config, headingYaw float32) error {
	ticker := time.NewTicker(time.Second / 20)
	defer ticker.Stop()

	scenarioAction, err := newScenarioAction(cfg, state, headingYaw)
	if err != nil {
		return err
	}
	runner := action.NewRunner(scenarioAction)
	if err := runner.Start(ctx); err != nil {
		return err
	}

	var tick uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick++
			if err := runner.Tick(ctx, action.TickContext{Tick: tick}); err != nil {
				return err
			}
			if err := writer.WritePacket(authInputPacket(state, tick)); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}
