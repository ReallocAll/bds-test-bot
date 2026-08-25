package bot

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const chunkWalkStepPerTick = float32(0.18)

type packetWriter interface {
	WritePacket(packet.Packet) error
}

type playerState struct {
	mu              sync.Mutex
	position        mgl32.Vec3
	pitch           float32
	yaw             float32
	headYaw         float32
	handledTeleport bool
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

func (s *playerState) inputSnapshot(scenario string, headingYaw float32) (mgl32.Vec3, float32, float32, float32, bool, mgl32.Vec3) {
	s.mu.Lock()
	defer s.mu.Unlock()

	teleport := s.handledTeleport
	s.handledTeleport = false
	pitch := s.pitch
	yaw := s.yaw
	headYaw := s.headYaw
	delta := mgl32.Vec3{}

	if scenario == ScenarioChunkWalk {
		yaw = headingYaw
		headYaw = headingYaw
		if !teleport {
			yawRad := float64(headingYaw) * math.Pi / 180
			delta = mgl32.Vec3{
				-float32(math.Sin(yawRad)) * chunkWalkStepPerTick,
				0,
				float32(math.Cos(yawRad)) * chunkWalkStepPerTick,
			}
			s.position[0] += delta[0]
			s.position[2] += delta[2]
		}
	}

	return s.position, pitch, yaw, headYaw, teleport, delta
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

func authInputPacket(s *playerState, tick uint64, scenario string, headingYaw float32) *packet.PlayerAuthInput {
	position, pitch, yaw, headYaw, handledTeleport, delta := s.inputSnapshot(scenario, headingYaw)
	flags := protocol.NewInputFlags(packet.InputFlagCount)
	if handledTeleport {
		flags.Set(packet.InputFlagHandledTeleport)
	}

	moveVector := mgl32.Vec2{}
	if scenario == ScenarioChunkWalk && !handledTeleport {
		flags.Set(packet.InputFlagUp)
		moveVector = mgl32.Vec2{0, 1}
	}

	pitchRad := float64(pitch) * math.Pi / 180
	yawRad := float64(yaw) * math.Pi / 180
	camera := mgl32.Vec3{
		-float32(math.Sin(yawRad) * math.Cos(pitchRad)),
		-float32(math.Sin(pitchRad)),
		float32(math.Cos(yawRad) * math.Cos(pitchRad)),
	}

	return &packet.PlayerAuthInput{
		Pitch:             pitch,
		Yaw:               yaw,
		Position:          position,
		MoveVector:        moveVector,
		HeadYaw:           headYaw,
		InputData:         flags,
		InputMode:         packet.InputModeMouse,
		PlayMode:          packet.PlayModeScreen,
		InteractionModel:  packet.InteractionModelCrosshair,
		InteractPitch:     pitch,
		InteractYaw:       yaw,
		Tick:              tick,
		Delta:             delta,
		AnalogueMoveVector: moveVector,
		CameraOrientation: camera,
		RawMoveVector:     moveVector,
	}
}

func runTickLoop(ctx context.Context, writer packetWriter, state *playerState, scenario string, headingYaw float32) error {
	ticker := time.NewTicker(time.Second / 20)
	defer ticker.Stop()

	var tick uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick++
			if err := writer.WritePacket(authInputPacket(state, tick, scenario, headingYaw)); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}
