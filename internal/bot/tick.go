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
	fly           bool
	flightTargetY float32
	verticalStep  float32
}

type playerState struct {
	mu                sync.Mutex
	position          mgl32.Vec3
	pitch             float32
	yaw               float32
	headYaw           float32
	handledTeleport   bool
	flyingConfirmed   bool
	serverCorrections uint64
	control           inputControl
}

type authInputSnapshot struct {
	position          mgl32.Vec3
	pitch             float32
	yaw               float32
	headYaw           float32
	handledTeleport   bool
	delta             mgl32.Vec3
	moveVector        mgl32.Vec2
	jumping           bool
	wasJumping        bool
	flightRequested   bool
	flyingConfirmed   bool
	verticalDirection float32
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
	s.serverCorrections++
}

func (s *playerState) setIdleControl() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.moveVector = mgl32.Vec2{}
	s.control.moveStep = 0
	s.control.overrideYaw = false
	s.control.overridePitch = false
	s.control.jump = false
	s.control.fly = false
}

func (s *playerState) setMoveControl(vector mgl32.Vec2, stepPerTick, yaw float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control.moveVector = vector
	s.control.moveStep = stepPerTick
	s.control.overrideYaw = true
	s.control.yaw = yaw
	s.control.fly = false
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

func (s *playerState) inputSnapshot() authInputSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := authInputSnapshot{
		position:        s.position,
		pitch:           s.pitch,
		yaw:             s.yaw,
		headYaw:         s.headYaw,
		handledTeleport: s.handledTeleport,
		flightRequested: s.control.fly,
		flyingConfirmed: s.flyingConfirmed,
	}
	s.handledTeleport = false

	if s.control.overridePitch {
		snapshot.pitch = s.control.pitch
	}
	if s.control.overrideYaw {
		snapshot.yaw = s.control.yaw
		snapshot.headYaw = s.control.yaw
	}

	snapshot.moveVector = s.control.moveVector
	if snapshot.handledTeleport {
		snapshot.moveVector = mgl32.Vec2{}
	} else if s.control.fly {
		if !s.flyingConfirmed {
			// Do not predict movement until the server has acknowledged creative
			// flight through UpdateAbilities. This prevents the local position
			// from racing ahead while BDS still considers the player grounded.
			snapshot.moveVector = mgl32.Vec2{}
		} else {
			diff := s.control.flightTargetY - s.position[1]
			if float32(math.Abs(float64(diff))) > 0.05 {
				snapshot.moveVector = mgl32.Vec2{}
				step := s.control.verticalStep
				if step <= 0 {
					step = float32(math.Abs(float64(diff)))
				}
				if float32(math.Abs(float64(diff))) < step {
					step = float32(math.Abs(float64(diff)))
				}
				if diff < 0 {
					step = -step
				}
				snapshot.delta[1] = step
				snapshot.verticalDirection = step
				s.position[1] += step
			} else {
				applyHorizontalMovement(s, &snapshot, s.control.moveStep)
			}
		}
	} else if snapshot.moveVector != (mgl32.Vec2{}) && s.control.moveStep != 0 {
		applyHorizontalMovement(s, &snapshot, s.control.moveStep)
	}

	snapshot.position = s.position
	snapshot.jumping = s.control.jump && !snapshot.handledTeleport
	snapshot.wasJumping = s.control.lastJump
	s.control.lastJump = snapshot.jumping
	return snapshot
}

func applyHorizontalMovement(s *playerState, snapshot *authInputSnapshot, stepPerTick float32) {
	if snapshot.moveVector == (mgl32.Vec2{}) || stepPerTick == 0 {
		return
	}
	yawRad := float64(snapshot.yaw) * math.Pi / 180
	forward := float64(snapshot.moveVector[1])
	strafe := float64(snapshot.moveVector[0])
	snapshot.delta[0] = float32(math.Cos(yawRad)*strafe-math.Sin(yawRad)*forward) * stepPerTick
	snapshot.delta[2] = float32(math.Sin(yawRad)*strafe+math.Cos(yawRad)*forward) * stepPerTick
	s.position[0] += snapshot.delta[0]
	s.position[2] += snapshot.delta[2]
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
	snapshot := s.inputSnapshot()
	flags := protocol.NewInputFlags(packet.InputFlagCount)
	if snapshot.handledTeleport {
		flags.Set(packet.InputFlagHandledTeleport)
	}

	if snapshot.moveVector[1] > 0 {
		flags.Set(packet.InputFlagUp)
	}
	if snapshot.moveVector[1] < 0 {
		flags.Set(packet.InputFlagDown)
	}
	if snapshot.moveVector[0] < 0 {
		flags.Set(packet.InputFlagLeft)
	}
	if snapshot.moveVector[0] > 0 {
		flags.Set(packet.InputFlagRight)
	}

	if snapshot.flightRequested && !snapshot.flyingConfirmed {
		flags.Set(packet.InputFlagStartFlying)
	}
	if snapshot.verticalDirection > 0 {
		flags.Set(packet.InputFlagAscend)
		flags.Set(packet.InputFlagWantUp)
	}
	if snapshot.verticalDirection < 0 {
		flags.Set(packet.InputFlagDescend)
		flags.Set(packet.InputFlagWantDown)
	}

	if snapshot.jumping {
		flags.Set(packet.InputFlagJumping)
		flags.Set(packet.InputFlagJumpCurrentRaw)
		if !snapshot.wasJumping {
			flags.Set(packet.InputFlagJumpDown)
			flags.Set(packet.InputFlagStartJumping)
			flags.Set(packet.InputFlagJumpPressedRaw)
		}
	} else if snapshot.wasJumping {
		flags.Set(packet.InputFlagJumpReleasedRaw)
	}

	pitchRad := float64(snapshot.pitch) * math.Pi / 180
	yawRad := float64(snapshot.yaw) * math.Pi / 180
	camera := mgl32.Vec3{
		-float32(math.Sin(yawRad) * math.Cos(pitchRad)),
		-float32(math.Sin(pitchRad)),
		float32(math.Cos(yawRad) * math.Cos(pitchRad)),
	}

	return &packet.PlayerAuthInput{
		Pitch:              snapshot.pitch,
		Yaw:                snapshot.yaw,
		Position:           snapshot.position,
		MoveVector:         snapshot.moveVector,
		HeadYaw:            snapshot.headYaw,
		InputData:          flags,
		InputMode:          packet.InputModeMouse,
		PlayMode:           packet.PlayModeScreen,
		InteractionModel:   packet.InteractionModelCrosshair,
		InteractPitch:      snapshot.pitch,
		InteractYaw:        snapshot.yaw,
		Tick:               tick,
		Delta:              snapshot.delta,
		AnalogueMoveVector: snapshot.moveVector,
		CameraOrientation:  camera,
		RawMoveVector:      snapshot.moveVector,
	}
}

func runTickLoop(ctx context.Context, writer packetWriter, state *playerState, cfg Config, headingYaw float32, botName string, entityRuntimeID uint64) error {
	ticker := time.NewTicker(time.Second / 20)
	defer ticker.Stop()

	scenarioAction, err := newScenarioAction(cfg, state, headingYaw, writer, botName, entityRuntimeID)
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
