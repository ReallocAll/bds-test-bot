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
	count         *atomic.Uint64
	movementCount *atomic.Uint64
}

func (w authInputWriter) WritePacket(pk packet.Packet) error {
	if err := w.writer.WritePacket(pk); err != nil {
		return err
	}
	w.count.Add(1)
	if input, ok := pk.(*packet.PlayerAuthInput); ok && (input.MoveVector[0] != 0 || input.MoveVector[1] != 0) {
		w.movementCount.Add(1)
	}
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
	defer func() {
		stats.AuthInputsSent = authInputs.Load()
		stats.MovementInputsSent = movementInputs.Load()
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
	state := newPlayerState(game.PlayerPosition, game.Pitch, game.Yaw)
	tickCtx, cancelTick := context.WithCancel(ctx)
	defer cancelTick()
	tickErr := make(chan error, 1)
	writer := authInputWriter{writer: conn, count: &authInputs, movementCount: &movementInputs}
	go func() { tickErr <- runTickLoop(tickCtx, writer, state, cfg, headingYaw) }()
	if err := out.Emit("scenario_started", map[string]any{"heading_yaw": headingYaw}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	worldDeadline := time.Now().Add(cfg.SpawnTimeout)
	if err := conn.SetReadDeadline(worldDeadline); err != nil {
		return stageError(ExitRuntime, "world", err)
	}

	chunkRadiusReady := false
	onlineState := false
	for {
		pk, readErr := conn.ReadPacket()
		if readErr != nil {
			cancelTick()
			if ctx.Err() != nil {
				<-gracefulCloseDone
				stats.AuthInputsSent = authInputs.Load()
				stats.MovementInputsSent = movementInputs.Load()
				_ = out.Emit("disconnected", map[string]any{
					"uptime":               time.Since(stats.StartedAt).Round(time.Millisecond).String(),
					"packets_received":     stats.PacketsReceived,
					"chunks_received":      stats.ChunksReceived,
					"auth_inputs_sent":     stats.AuthInputsSent,
					"movement_inputs_sent": stats.MovementInputsSent,
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
			// Initial chunk streaming becomes very noisy for larger fleets. Keep
			// representative evidence while retaining the exact per-bot counter.
			if stats.ChunksReceived <= 3 || stats.ChunksReceived%100 == 0 {
				if err := out.Emit("chunk_received", map[string]any{
					"x": p.Position[0], "z": p.Position[1], "total": stats.ChunksReceived,
				}); err != nil {
					return stageError(ExitRuntime, "output", err)
				}
			}
		case *packet.MovePlayer:
			if p.EntityRuntimeID == game.EntityRuntimeID {
				state.update(p.Position, p.Pitch, p.Yaw, p.HeadYaw, p.Mode == packet.MoveModeTeleport)
			}
		case *packet.CorrectPlayerMovePrediction:
			if p.PredictionType == packet.PredictionTypePlayer {
				state.update(p.Position, p.Rotation[0], p.Rotation[1], p.Rotation[1], false)
			}
		}

		if !onlineState && chunkRadiusReady && stats.ChunksReceived > 0 {
			onlineState = true
			stats.OnlineAt = time.Now()
			stats.AuthInputsSent = authInputs.Load()
			stats.MovementInputsSent = movementInputs.Load()
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				return stageError(ExitRuntime, "world", err)
			}
			if err := out.Emit("online", map[string]any{
				"chunks_received":      stats.ChunksReceived,
				"packets_received":     stats.PacketsReceived,
				"auth_inputs_sent":     stats.AuthInputsSent,
				"movement_inputs_sent": stats.MovementInputsSent,
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
