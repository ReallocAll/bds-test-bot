package bot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	ExitRuntime = 1
	ExitArgs    = 2
	ExitConnect = 3
	ExitSpawn   = 4

	// gophertunnel ultimately closes its transport through go-raknet. go-raknet
	// intentionally makes Close asynchronous so outstanding reliable datagrams
	// can be acknowledged before it sends the RakNet DisconnectNotification.
	// Keep the process alive briefly after initiating the close; otherwise the
	// Go runtime terminates that transport goroutine and BDS only removes the
	// player after its network timeout.
	rakNetCloseGrace = 2 * time.Second
)

type eventEmitter interface {
	Emit(string, map[string]any) error
}

type StageError struct {
	Code  int
	Stage string
	Err   error
}

func (e *StageError) Error() string {
	return fmt.Sprintf("%s: %v", e.Stage, e.Err)
}

func (e *StageError) Unwrap() error { return e.Err }

func stageError(code int, stage string, err error) error {
	return &StageError{Code: code, Stage: stage, Err: err}
}

type authInputWriter struct {
	writer        packetWriter
	authCount     *atomic.Uint64
	movementCount *atomic.Uint64
	actionCount   *atomic.Uint64
}

func (w authInputWriter) WritePacket(pk packet.Packet) error {
	if err := w.writer.WritePacket(pk); err != nil {
		return err
	}
	if input, ok := pk.(*packet.PlayerAuthInput); ok {
		w.authCount.Add(1)
		moving := input.MoveVector[0] != 0 || input.MoveVector[1] != 0 ||
			input.Delta[0] != 0 || input.Delta[1] != 0 || input.Delta[2] != 0 ||
			input.InputData.Load(packet.InputFlagAscend) || input.InputData.Load(packet.InputFlagDescend) ||
			input.InputData.Load(packet.InputFlagWantUp) || input.InputData.Load(packet.InputFlagWantDown)
		if moving {
			w.movementCount.Add(1)
		}
		return nil
	}
	w.actionCount.Add(1)
	return nil
}

func runInstance(
	ctx context.Context,
	cfg Config,
	name string,
	instanceIndex int,
	out eventEmitter,
	stats *InstanceStats,
	online chan<- InstanceStats,
) error {
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	if stats.StartedAt.IsZero() {
		stats.StartedAt = time.Now()
	}
	var authInputs atomic.Uint64
	var movementInputs atomic.Uint64
	var actionPackets atomic.Uint64
	var state *playerState
	defer func() {
		stats.AuthInputsSent = authInputs.Load()
		stats.MovementInputsSent = movementInputs.Load()
		stats.ActionPacketsSent = actionPackets.Load()
		if state != nil {
			position, flying, corrections := state.telemetrySnapshot()
			stats.FinalPosition = position
			stats.FlyingConfirmed = flying
			stats.ServerCorrections = corrections
			stats.HorizontalDistance = horizontalDistance(stats.StartPosition, position)
		}
	}()

	if err := out.Emit("connecting", map[string]any{"address": address, "name": name}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	startGameSeen := make(chan struct{})
	var startGameOnce sync.Once
	dialer := minecraft.Dialer{
		IdentityData: login.IdentityData{DisplayName: name},
		PacketFunc: func(header packet.Header, _ []byte, _, _ net.Addr) {
			if header.PacketID == packet.IDStartGame {
				startGameOnce.Do(func() { close(startGameSeen) })
			}
		},
	}

	connectCtx, cancelConnect := context.WithTimeout(ctx, cfg.ConnectTimeout)
	conn, err := dialer.DialContext(connectCtx, "raknet", address)
	cancelConnect()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return stageError(ExitConnect, "connect", err)
	}
	defer conn.Close()
	if err := out.Emit("connected", nil); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	closeOnCancelDone := make(chan struct{})
	gracefulCloseDone := make(chan struct{})
	defer close(closeOnCancelDone)
	go func() {
		defer close(gracefulCloseDone)
		select {
		case <-ctx.Done():
			_ = conn.Close()
			// minecraft.Conn.Close() returns before go-raknet has necessarily
			// sent its transport-level DisconnectNotification. Do not let the
			// process exit while that asynchronous close is still in flight.
			time.Sleep(rakNetCloseGrace)
		case <-closeOnCancelDone:
		}
	}()

	spawnCtx, cancelSpawn := context.WithTimeout(ctx, cfg.SpawnTimeout)
	err = conn.DoSpawnContext(spawnCtx)
	cancelSpawn()
	if err != nil {
		if ctx.Err() != nil {
			<-gracefulCloseDone
			return nil
		}
		return stageError(ExitSpawn, "spawn", err)
	}
	select {
	case <-startGameSeen:
		if err := out.Emit("start_game", nil); err != nil {
			return stageError(ExitRuntime, "output", err)
		}
	default:
		return stageError(ExitSpawn, "spawn", errors.New("spawn completed without observing StartGame"))
	}

	game := conn.GameData()
	stats.StartPosition = game.PlayerPosition
	if err := out.Emit("spawned", map[string]any{
		"x": game.PlayerPosition[0], "y": game.PlayerPosition[1], "z": game.PlayerPosition[2],
	}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	if err := conn.WritePacket(&packet.RequestChunkRadius{
		ChunkRadius:    cfg.ChunkRadius,
		MaxChunkRadius: uint8(cfg.ChunkRadius),
	}); err != nil {
		return stageError(ExitRuntime, "chunk-radius", err)
	}
	if err := out.Emit("chunk_radius_requested", map[string]any{"radius": cfg.ChunkRadius}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	headingYaw := scenarioHeading(game.Yaw, instanceIndex, cfg.Count)
	state = newPlayerState(game.PlayerPosition, game.Pitch, game.Yaw)
	tickCtx, cancelTick := context.WithCancel(ctx)
	defer cancelTick()
	tickErr := make(chan error, 1)
	writer := authInputWriter{
		writer:        conn,
		authCount:     &authInputs,
		movementCount: &movementInputs,
		actionCount:   &actionPackets,
	}
	go func() {
		tickErr <- runTickLoop(tickCtx, writer, state, cfg, headingYaw, name, game.EntityRuntimeID)
	}()
	if err := out.Emit("scenario_started", map[string]any{"heading_yaw": headingYaw}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	worldDeadline := time.Now().Add(cfg.SpawnTimeout)
	if err := conn.SetReadDeadline(worldDeadline); err != nil {
		return stageError(ExitRuntime, "world", err)
	}

	chunkRadiusReady := false
	onlineState := false
	nextProgress := time.Now().Add(5 * time.Second)
	var publisherUpdates uint64
	var postMoveUpdates uint64
	var postMoveX, postMoveY, postMoveZ float32
	postMoveSet := false
	var publisherX, publisherY, publisherZ int32
	var publisherChunkX, publisherChunkZ int32
	publisherSet := false
	for {
		pk, readErr := conn.ReadPacket()
		if readErr != nil {
			cancelTick()
			if ctx.Err() != nil {
				<-gracefulCloseDone
				stats.AuthInputsSent = authInputs.Load()
				stats.MovementInputsSent = movementInputs.Load()
				stats.ActionPacketsSent = actionPackets.Load()
				_ = out.Emit("disconnected", map[string]any{
					"uptime":               time.Since(stats.StartedAt).Round(time.Millisecond).String(),
					"packets_received":     stats.PacketsReceived,
					"chunks_received":      stats.ChunksReceived,
					"auth_inputs_sent":     stats.AuthInputsSent,
					"movement_inputs_sent": stats.MovementInputsSent,
					"action_packets_sent":  stats.ActionPacketsSent,
					"was_online":           onlineState,
				})
				return nil
			}
			if !onlineState && time.Now().After(worldDeadline) {
				return stageError(ExitSpawn, "world", fmt.Errorf("timed out waiting for chunk radius and first chunk: %w", readErr))
			}
			return stageError(ExitRuntime, "read", readErr)
		}
		stats.PacketsReceived++

		switch p := pk.(type) {
		case *packet.ChunkRadiusUpdated:
			chunkRadiusReady = true
			if err := out.Emit("chunk_radius", map[string]any{"radius": p.ChunkRadius}); err != nil {
				return stageError(ExitRuntime, "output", err)
			}
		case *packet.LevelChunk:
			stats.ChunksReceived++
			stats.recordChunk(p.Position[0], p.Position[1])
			// Initial chunk streaming becomes very noisy for larger fleets. Keep
			// representative evidence while retaining the exact per-bot counter.
			if stats.ChunksReceived <= 3 || stats.ChunksReceived%100 == 0 {
				if err := out.Emit("chunk_received", map[string]any{
					"x": p.Position[0], "z": p.Position[1], "total": stats.ChunksReceived,
				}); err != nil {
					return stageError(ExitRuntime, "output", err)
				}
			}
		case *packet.NetworkChunkPublisherUpdate:
			publisherUpdates++
			x, y, z := p.Position[0], p.Position[1], p.Position[2]
			chunkX, chunkZ := x>>4, z>>4
			samePublisher := publisherSet && x == publisherX && y == publisherY && z == publisherZ
			changedChunk := !publisherSet || chunkX != publisherChunkX || chunkZ != publisherChunkZ
			if cfg.Scenario == ScenarioChunkFly && samePublisher {
				// BDS currently sends an initial one-shot publisher at 0,0,0, then
				// repeatedly publishes the real player block position. Two identical
				// consecutive publisher positions give us server-owned spawn evidence
				// without trusting StartGame's Y≈32768 placeholder. The publisher
				// is a server-owned BlockPos and may differ in X/Z after BDS resolves
				// the real spawn. Seed all axes from it: block centres for X/Z and
				// eye height for the PlayerAuthInput Y coordinate.
				candidate := publisherEyePosition(p.Position)
				if state.acceptPublisherPosition(candidate) {
					stats.StartPosition = candidate
					if err := out.Emit("authoritative_position", map[string]any{
						"position": []float32{candidate[0], candidate[1], candidate[2]},
						"source":   "chunk_publisher",
					}); err != nil {
						return stageError(ExitRuntime, "output", err)
					}
				}
			}
			publisherX, publisherY, publisherZ = x, y, z
			publisherChunkX, publisherChunkZ = chunkX, chunkZ
			publisherSet = true
			if cfg.Scenario == ScenarioChunkFly && changedChunk {
				if err := out.Emit("chunk_publisher", map[string]any{
					"x": x, "y": y, "z": z, "chunk_x": chunkX, "chunk_z": chunkZ,
					"radius": p.Radius, "updates": publisherUpdates,
				}); err != nil {
					return stageError(ExitRuntime, "output", err)
				}
			}
		case *packet.ServerPlayerPostMovePosition:
			postMoveUpdates++
			postMoveX, postMoveY, postMoveZ = p.Position[0], p.Position[1], p.Position[2]
			postMoveSet = true
			if cfg.Scenario == ScenarioChunkFly && (postMoveUpdates <= 3 || postMoveUpdates%100 == 0) {
				if err := out.Emit("server_post_move", map[string]any{
					"position": []float32{postMoveX, postMoveY, postMoveZ},
					"updates":  postMoveUpdates,
				}); err != nil {
					return stageError(ExitRuntime, "output", err)
				}
			}
		case *packet.MovePlayer:
			if p.EntityRuntimeID == game.EntityRuntimeID {
				state.update(p.Position, p.Pitch, p.Yaw, p.HeadYaw, p.Mode == packet.MoveModeTeleport)
				state.syncServerTick(p.Tick)
			}
		case *packet.CorrectPlayerMovePrediction:
			if p.PredictionType == packet.PredictionTypePlayer {
				state.correct(p.Position, p.Rotation[0], p.Rotation[1], p.Rotation[1])
				state.syncServerTick(p.Tick)
			}
		case *packet.UpdateAttributes:
			if p.EntityRuntimeID == game.EntityRuntimeID {
				state.syncServerTick(p.Tick)
			}
		case *packet.SetActorData:
			if p.EntityRuntimeID == game.EntityRuntimeID {
				state.syncServerTick(p.Tick)
			}
		case *packet.SetActorMotion:
			if p.EntityRuntimeID == game.EntityRuntimeID {
				state.syncServerTick(p.Tick)
			}
		case *packet.MobEffect:
			if p.EntityRuntimeID == game.EntityRuntimeID {
				state.syncServerTick(p.Tick)
			}
		case *packet.UpdatePlayerGameType:
			if p.PlayerUniqueID == game.EntityUniqueID {
				state.syncServerTick(p.Tick)
			}
		case *packet.UpdateAbilities:
			if cfg.Scenario == ScenarioChunkFly && (p.AbilityData.EntityUniqueID == game.EntityUniqueID || p.AbilityData.EntityUniqueID == 0) {
				mayFly, flying := flightAbilityState(p.AbilityData)
				if state.setFlyingConfirmed(flying) {
					if err := out.Emit("flight_state", map[string]any{
						"may_fly": mayFly,
						"flying":  flying,
					}); err != nil {
						return stageError(ExitRuntime, "output", err)
					}
				}
			}
		}

		if !onlineState && chunkRadiusReady && stats.ChunksReceived > 0 {
			onlineState = true
			stats.OnlineAt = time.Now()
			stats.AuthInputsSent = authInputs.Load()
			stats.MovementInputsSent = movementInputs.Load()
			stats.ActionPacketsSent = actionPackets.Load()
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				return stageError(ExitRuntime, "world", err)
			}
			if err := out.Emit("online", map[string]any{
				"chunks_received":      stats.ChunksReceived,
				"packets_received":     stats.PacketsReceived,
				"auth_inputs_sent":     stats.AuthInputsSent,
				"movement_inputs_sent": stats.MovementInputsSent,
				"action_packets_sent":  stats.ActionPacketsSent,
				"uptime":               time.Since(stats.StartedAt).Round(time.Millisecond).String(),
			}); err != nil {
				return stageError(ExitRuntime, "output", err)
			}
			if online != nil {
				select {
				case online <- *stats:
				case <-ctx.Done():
				}
			}
		}

		if onlineState && cfg.Scenario == ScenarioChunkFly && !time.Now().Before(nextProgress) {
			position, flying, corrections := state.telemetrySnapshot()
			nextInputTick, tickSynced := state.tickSnapshot()
			positionReady := state.positionReadySnapshot()
			spanX, spanZ := stats.chunkSpan()
			fields := map[string]any{
				"position":                 []float32{position[0], position[1], position[2]},
				"position_ready":           positionReady,
				"horizontal_distance":      horizontalDistance(stats.StartPosition, position),
				"flying_confirmed":         flying,
				"server_corrections":       corrections,
				"chunks_received":          stats.ChunksReceived,
				"chunk_span_x":             spanX,
				"chunk_span_z":             spanZ,
				"auth_inputs_sent":         authInputs.Load(),
				"movement_inputs_sent":     movementInputs.Load(),
				"next_input_tick":          nextInputTick,
				"tick_synced":              tickSynced,
				"publisher_updates":        publisherUpdates,
				"server_post_move_updates": postMoveUpdates,
			}
			if postMoveSet {
				fields["server_post_move_position"] = []float32{postMoveX, postMoveY, postMoveZ}
			}
			if publisherSet {
				fields["publisher_position"] = []int32{publisherX, publisherY, publisherZ}
				fields["publisher_chunk"] = []int32{publisherChunkX, publisherChunkZ}
			}
			if err := out.Emit("bot_progress", fields); err != nil {
				return stageError(ExitRuntime, "output", err)
			}
			nextProgress = time.Now().Add(5 * time.Second)
		}

		select {
		case err := <-tickErr:
			if err != nil {
				return stageError(ExitRuntime, "tick", err)
			}
			if ctx.Err() != nil {
				<-gracefulCloseDone
				return nil
			}
			return stageError(ExitRuntime, "tick", errors.New("tick loop exited unexpectedly"))
		default:
		}
	}
}
