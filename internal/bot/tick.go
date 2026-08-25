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

func (s *playerState) snapshot() (mgl32.Vec3, float32, float32, float32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	teleport := s.handledTeleport
	s.handledTeleport = false
	return s.position, s.pitch, s.yaw, s.headYaw, teleport
}

func authInputPacket(s *playerState, tick uint64) *packet.PlayerAuthInput {
	position, pitch, yaw, headYaw, handledTeleport := s.snapshot()
	flags := protocol.NewInputFlags(packet.InputFlagCount)
	if handledTeleport {
		flags.Set(packet.InputFlagHandledTeleport)
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
		HeadYaw:           headYaw,
		InputData:         flags,
		InputMode:         packet.InputModeMouse,
		PlayMode:          packet.PlayModeScreen,
		InteractionModel:  packet.InteractionModelCrosshair,
		InteractPitch:     pitch,
		InteractYaw:       yaw,
		Tick:              tick,
		CameraOrientation: camera,
	}
}

func runTickLoop(ctx context.Context, writer packetWriter, state *playerState) error {
	ticker := time.NewTicker(time.Second / 20)
	defer ticker.Stop()

	var tick uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick++
			if err := writer.WritePacket(authInputPacket(state, tick)); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}
