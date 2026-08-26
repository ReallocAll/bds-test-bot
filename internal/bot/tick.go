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

const chunkWalkStepPerTick = float32(4.317 / 20)
const movementStartDelay = 2 * time.Second

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

type predictionFrame struct {
	tick  uint64
	delta mgl32.Vec3
}

const predictionHistoryLimit = 256

type playerState struct {
	mu                  sync.Mutex
	position            mgl32.Vec3
	positionReady       bool
	inputStarted        bool
	correctionProbeSent bool
	pitch               float32
	yaw                 float32
	headYaw             float32
	handledTeleport     bool
	flyingConfirmed     bool
	flightStartPending  bool
	serverCorrections   uint64
	serverTick          uint64
	tickSynced          bool
	predictionHistory   []predictionFrame
	control             inputControl
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
	startFlying       bool
	flyingConfirmed   bool
	correctionProbe   bool
	verticalDirection float32
	committedDelta    mgl32.Vec3
}

func validPlayerPositionY(y float32) bool {
	return y >= chunkFlyValidMinY && y <= chunkFlyValidMaxY
}

func newPlayerState(position mgl32.Vec3, pitch, yaw float32) *playerState {
	return &playerState{
		position:      position,
		positionReady: validPlayerPositionY(position[1]),
		pitch:         pitch,
		yaw:           yaw,
		headYaw:       yaw,
	}
}

func (s *playerState) update(position mgl32.Vec3, pitch, yaw, headYaw float32, teleport bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.position = position
	if validPlayerPositionY(position[1]) {
		s.positionReady = true
	}
	s.pitch = pitch
	s.yaw = yaw
	s.headYaw = headYaw
	s.handledTeleport = s.handledTeleport || teleport
	if teleport {
		s.serverCorrections++
	}
}

func (s *playerState) correct(position mgl32.Vec3, pitch, yaw, headYaw float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.position = position
	if validPlayerPositionY(position[1]) {
		s.positionReady = true
	}
	s.pitch = pitch
	s.yaw = yaw
	s.headYaw = headYaw
	s.serverCorrections++
}

func (s *playerState) recordPrediction(tick uint64, delta mgl32.Vec3) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.predictionHistory = append(s.predictionHistory, predictionFrame{tick: tick, delta: delta})
	if len(s.predictionHistory) > predictionHistoryLimit {
		s.predictionHistory = append([]predictionFrame(nil), s.predictionHistory[len(s.predictionHistory)-predictionHistoryLimit:]...)
	}
}

func (s *playerState) correctPrediction(position mgl32.Vec3, pitch, yaw, headYaw float32, tick uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := position
	keep := s.predictionHistory[:0]
	for _, frame := range s.predictionHistory {
		if frame.tick > tick {
			current = current.Add(frame.delta)
			keep = append(keep, frame)
		}
	}
	s.predictionHistory = keep
	s.position = current
	if validPlayerPositionY(position[1]) {
		s.positionReady = true
	}
	s.pitch = pitch
	s.yaw = yaw
	s.headYaw = headYaw
	s.serverCorrections++
}

func (s *playerState) acceptPublisherPosition(position mgl32.Vec3) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.positionReady || s.inputStarted || !validPlayerPositionY(position[1]) {
		return false
	}
	// Publisher position is only a pre-input fallback. Once PlayerAuthInput has
	// started, BDS correction/movement packets own prediction history and the
	// publisher remains observability-only evidence.
	s.position = position
	s.positionReady = true
	return true
}

func (s *playerState) observePublisherPosition(position mgl32.Vec3) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.positionReady || !validPlayerPositionY(position[1]) {
		return false
	}
	changed := s.position != position
	s.position = position
	return changed
}

func (s *playerState) positionReadySnapshot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.positionReady
}

func (s *playerState) syncServerTick(serverTick uint64) {
	if serverTick == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := serverTick + 1
	if !s.tickSynced || next > s.serverTick {
		s.serverTick = next
		s.tickSynced = true
	}
}

func (s *playerState) nextInputTick() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputStarted = true
	// PlayerAuthInput carries the client's monotonically advancing movement
	// frame id. Server packets may move this clock forward when they refer to
	// a later prediction, but initial movement starts at frame zero.
	tick := s.serverTick
	s.serverTick++
	return tick
}

func (s *playerState) tickSnapshot() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverTick, s.tickSynced
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
	s.flightStartPending = false
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

func (s *playerState) queueFlightStartTransition() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flightStartPending = true
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
		startFlying:     s.flightStartPending,
		flyingConfirmed: s.flyingConfirmed,
	}
	s.handledTeleport = false
	s.flightStartPending = false

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
		switch {
		case !s.flyingConfirmed:
			snapshot.moveVector = mgl32.Vec2{}
		case !s.positionReady && !s.correctionProbeSent:
			// A real reference client only receives its initial
			// CorrectPlayerMovePrediction after the first non-zero movement frame.
			// Send exactly one coherent post-move probe from the StartGame
			// placeholder, but do not commit that speculative displacement to
			// s.position. Until BDS corrects us, subsequent frames stay idle.
			s.correctionProbeSent = true
			snapshot.correctionProbe = true
			applyProbeMovement(&snapshot, s.control.moveStep)
		case !s.positionReady:
			snapshot.moveVector = mgl32.Vec2{}
		default:
			// Creative flight is server-confirmed before prediction begins. Climb
			// vertically to the safe target first so terrain cannot pin the bot,
			// then begin horizontal chunk traversal.
			applyFlightMovement(s, &snapshot)
		}
	} else if snapshot.moveVector != (mgl32.Vec2{}) && s.control.moveStep != 0 {
		applyHorizontalMovement(s, &snapshot, s.control.moveStep)
	}

	if snapshot.correctionProbe {
		snapshot.position = s.position.Add(snapshot.delta)
	} else {
		snapshot.position = s.position
	}
	snapshot.jumping = s.control.jump && !snapshot.handledTeleport
	snapshot.wasJumping = s.control.lastJump
	s.control.lastJump = snapshot.jumping
	return snapshot
}

func applyProbeMovement(snapshot *authInputSnapshot, stepPerTick float32) {
	if snapshot.moveVector == (mgl32.Vec2{}) || stepPerTick == 0 {
		return
	}
	yawRad := float64(snapshot.yaw) * math.Pi / 180
	forward := float64(snapshot.moveVector[1])
	strafe := float64(snapshot.moveVector[0])
	snapshot.delta[0] = float32(math.Cos(yawRad)*strafe-math.Sin(yawRad)*forward) * stepPerTick
	snapshot.delta[2] = float32(math.Sin(yawRad)*strafe+math.Cos(yawRad)*forward) * stepPerTick
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
	snapshot.committedDelta[0] = snapshot.delta[0]
	snapshot.committedDelta[2] = snapshot.delta[2]
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
	flags.Set(packet.InputFlagBlockBreakingDelayEnabled)
	if snapshot.moveVector != (mgl32.Vec2{}) && !snapshot.flightRequested {
		flags.Set(packet.InputFlagVerticalCollision)
		snapshot.delta[1] = -0.08 * 0.98
	}
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

	if snapshot.startFlying {
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

	moveVector := snapshot.moveVector
	rawMoveVector := mgl32.Vec2{}
	if lengthSquared := moveVector.LenSqr(); lengthSquared > 0 {
		moveVector = moveVector.Mul(1 / float32(math.Sqrt(float64(lengthSquared))))
		horizontalDelta := float32(math.Hypot(float64(snapshot.delta[0]), float64(snapshot.delta[2])))
		rawMoveVector = moveVector.Mul(horizontalDelta)
	}

	s.recordPrediction(tick, snapshot.committedDelta)

	// Match the coherent input tuple used by go-test-bds, a current BDS test
	// client on the same gophertunnel protocol family. In particular RawMoveVector
	// is the local movement magnitude for this tick, while MoveVector is the
	// normalised direction; reporting a full-scale raw stick alongside a much
	// smaller predicted Delta makes the packet internally inconsistent.
	return &packet.PlayerAuthInput{
		Pitch:             snapshot.pitch,
		Yaw:               snapshot.yaw,
		Position:          snapshot.position,
		MoveVector:        moveVector,
		HeadYaw:           snapshot.headYaw,
		InputData:         flags,
		InputMode:         packet.InputModeMouse,
		PlayMode:          packet.PlayModeScreen,
		InteractionModel:  packet.InteractionModelTouch,
		InteractPitch:     snapshot.pitch,
		InteractYaw:       snapshot.yaw,
		Tick:              tick,
		Delta:             snapshot.delta,
		CameraOrientation: camera,
		RawMoveVector:     rawMoveVector,
	}
}

func runTickLoop(ctx context.Context, writer packetWriter, state *playerState, cfg Config, headingYaw float32, botName string, entityRuntimeID uint64) error {
	// Preserve the settle delay for walking. Chunk-fly starts an idle auth frame
	// immediately, then requests flight. Once BDS confirms the ability, the first
	// non-zero frame is a single correction probe matching the reference client's
	// post-physics PlayerAuthInput convention.
	if cfg.Scenario == ScenarioChunkWalk {
		timer := time.NewTimer(movementStartDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}

	if cfg.Scenario == ScenarioChunkFly {
		tick := state.nextInputTick()
		if err := writer.WritePacket(authInputPacket(state, tick)); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}

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

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick := state.nextInputTick()
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
