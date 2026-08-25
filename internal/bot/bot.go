package bot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"

	"github.com/ReallocAll/bds-test-bot/internal/output"
)

const (
	ExitRuntime = 1
	ExitArgs    = 2
	ExitConnect = 3
	ExitSpawn   = 4
)

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

func Run(ctx context.Context, cfg Config, out *output.Emitter) error {
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	startedAt := time.Now()
	if err := out.Emit("connecting", map[string]any{"address": address, "name": cfg.Name}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	startGameSeen := make(chan struct{})
	var startGameOnce sync.Once
	dialer := minecraft.Dialer{
		IdentityData: login.IdentityData{DisplayName: cfg.Name},
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
	defer close(closeOnCancelDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-closeOnCancelDone:
		}
	}()

	spawnCtx, cancelSpawn := context.WithTimeout(ctx, cfg.SpawnTimeout)
	err = conn.DoSpawnContext(spawnCtx)
	cancelSpawn()
	if err != nil {
		if ctx.Err() != nil {
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
		ChunkRadius:    uint32(cfg.ChunkRadius),
		MaxChunkRadius: uint32(cfg.ChunkRadius),
	}); err != nil {
		return stageError(ExitRuntime, "chunk-radius", err)
	}
	if err := out.Emit("chunk_radius_requested", map[string]any{"radius": cfg.ChunkRadius}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	state := newPlayerState(game.PlayerPosition, game.Pitch, game.Yaw)
	tickCtx, cancelTick := context.WithCancel(ctx)
	defer cancelTick()
	tickErr := make(chan error, 1)
	go func() { tickErr <- runTickLoop(tickCtx, conn, state) }()

	worldDeadline := time.Now().Add(cfg.SpawnTimeout)
	if err := conn.SetReadDeadline(worldDeadline); err != nil {
		return stageError(ExitRuntime, "world", err)
	}

	var packetsReceived uint64
	var chunksReceived uint64
	chunkRadiusReady := false
	online := false
	for {
		pk, readErr := conn.ReadPacket()
		if readErr != nil {
			cancelTick()
			if ctx.Err() != nil {
				_ = out.Emit("disconnected", map[string]any{"uptime": time.Since(startedAt).Round(time.Millisecond).String()})
				return nil
			}
			if !online && time.Now().After(worldDeadline) {
				return stageError(ExitSpawn, "world", fmt.Errorf("timed out waiting for chunk radius and first chunk: %w", readErr))
			}
			return stageError(ExitRuntime, "read", readErr)
		}
		packetsReceived++

		switch p := pk.(type) {
		case *packet.ChunkRadiusUpdated:
			chunkRadiusReady = true
			if err := out.Emit("chunk_radius", map[string]any{"radius": p.ChunkRadius}); err != nil {
				return stageError(ExitRuntime, "output", err)
			}
		case *packet.LevelChunk:
			chunksReceived++
			if err := out.Emit("chunk_received", map[string]any{
				"x": p.Position[0], "z": p.Position[1], "total": chunksReceived,
			}); err != nil {
				return stageError(ExitRuntime, "output", err)
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

		if !online && chunkRadiusReady && chunksReceived > 0 {
			online = true
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				return stageError(ExitRuntime, "world", err)
			}
			if err := out.Emit("online", map[string]any{
				"chunks_received": chunksReceived,
				"packets_received": packetsReceived,
				"uptime": time.Since(startedAt).Round(time.Millisecond).String(),
			}); err != nil {
				return stageError(ExitRuntime, "output", err)
			}
		}

		select {
		case err := <-tickErr:
			if err != nil {
				return stageError(ExitRuntime, "tick", err)
			}
			if ctx.Err() != nil {
				return nil
			}
		default:
		}
	}
}
