package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/ReallocAll/bds-test-bot/internal/output"
	"github.com/go-gl/mathgl/mgl32"
)

type InstanceStats struct {
	Name               string
	Index              int
	Scenario           string
	StartedAt          time.Time
	OnlineAt           time.Time
	EndedAt            time.Time
	PacketsReceived    uint64
	ChunksReceived     uint64
	AuthInputsSent     uint64
	MovementInputsSent uint64
	ActionPacketsSent  uint64

	StartPosition      mgl32.Vec3
	FinalPosition      mgl32.Vec3
	HorizontalDistance float64
	FlyingConfirmed    bool
	ServerCorrections  uint64
	ChunkBoundsSet     bool
	ChunkMinX          int32
	ChunkMaxX          int32
	ChunkMinZ          int32
	ChunkMaxZ          int32
}

func (s *InstanceStats) recordChunk(x, z int32) {
	if !s.ChunkBoundsSet {
		s.ChunkBoundsSet = true
		s.ChunkMinX, s.ChunkMaxX = x, x
		s.ChunkMinZ, s.ChunkMaxZ = z, z
		return
	}
	if x < s.ChunkMinX {
		s.ChunkMinX = x
	}
	if x > s.ChunkMaxX {
		s.ChunkMaxX = x
	}
	if z < s.ChunkMinZ {
		s.ChunkMinZ = z
	}
	if z > s.ChunkMaxZ {
		s.ChunkMaxZ = z
	}
}

func (s InstanceStats) chunkSpan() (int32, int32) {
	if !s.ChunkBoundsSet {
		return 0, 0
	}
	return s.ChunkMaxX - s.ChunkMinX + 1, s.ChunkMaxZ - s.ChunkMinZ + 1
}

type instanceResult struct {
	stats InstanceStats
	err   error
}

type scopedEmitter struct {
	base     *output.Emitter
	name     string
	index    int
	scenario string
}

func (e scopedEmitter) Emit(event string, fields map[string]any) error {
	enriched := make(map[string]any, len(fields)+3)
	for key, value := range fields {
		enriched[key] = value
	}
	enriched["bot"] = e.name
	enriched["index"] = e.index
	enriched["scenario"] = e.scenario
	return e.base.Emit(event, enriched)
}

func Run(ctx context.Context, cfg Config, out *output.Emitter) error {
	fleetCtx, cancelFleet := context.WithCancel(ctx)
	defer cancelFleet()

	onlineCh := make(chan InstanceStats, cfg.Count)
	resultCh := make(chan instanceResult, cfg.Count)
	results := make([]instanceResult, 0, cfg.Count)
	launched := 0
	onlineCount := 0

	if err := out.Emit("fleet_starting", map[string]any{
		"count":         cfg.Count,
		"name_prefix":   cfg.NamePrefix,
		"scenario":      cfg.Scenario,
		"login_stagger": cfg.LoginStagger.String(),
	}); err != nil {
		return stageError(ExitRuntime, "output", err)
	}

	launch := func(index int) error {
		name := instanceName(cfg, index)
		stats := &InstanceStats{
			Name:      name,
			Index:     index + 1,
			Scenario:  cfg.Scenario,
			StartedAt: time.Now(),
		}
		scoped := scopedEmitter{base: out, name: name, index: index + 1, scenario: cfg.Scenario}
		if err := out.Emit("fleet_login_started", map[string]any{
			"bot": name, "index": index + 1, "count": cfg.Count,
		}); err != nil {
			return stageError(ExitRuntime, "output", err)
		}
		launched++
		go func() {
			err := runInstance(fleetCtx, cfg, name, index+1, scoped, stats, onlineCh)
			stats.EndedAt = time.Now()
			resultCh <- instanceResult{stats: *stats, err: err}
		}()
		return nil
	}

	recordOnline := func(stats InstanceStats) error {
		onlineCount++
		fields := map[string]any{
			"online":               onlineCount,
			"count":                cfg.Count,
			"bot":                  stats.Name,
			"index":                stats.Index,
			"chunks_received":      stats.ChunksReceived,
			"auth_inputs_sent":     stats.AuthInputsSent,
			"movement_inputs_sent": stats.MovementInputsSent,
			"action_packets_sent":  stats.ActionPacketsSent,
		}
		if stats.Scenario == ScenarioChunkFly {
			spanX, spanZ := stats.chunkSpan()
			fields["flying_confirmed"] = stats.FlyingConfirmed
			fields["chunk_span_x"] = spanX
			fields["chunk_span_z"] = spanZ
		}
		return out.Emit("fleet_progress", fields)
	}

	for i := 0; i < cfg.Count; i++ {
		if ctx.Err() != nil {
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, nil)
		}
		if err := launch(i); err != nil {
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, err)
		}
		if i+1 == cfg.Count || cfg.LoginStagger == 0 {
			continue
		}

		timer := time.NewTimer(cfg.LoginStagger)
		select {
		case <-timer.C:
		case stats := <-onlineCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if err := recordOnline(stats); err != nil {
				return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, stageError(ExitRuntime, "output", err))
			}
		case result := <-resultCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			results = append(results, result)
			err := result.err
			if err == nil {
				err = stageError(ExitRuntime, "fleet", fmt.Errorf("%s exited before the fleet became online", result.stats.Name))
			}
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, err)
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, nil)
		}
	}

	for onlineCount < cfg.Count {
		select {
		case stats := <-onlineCh:
			if err := recordOnline(stats); err != nil {
				return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, stageError(ExitRuntime, "output", err))
			}
		case result := <-resultCh:
			results = append(results, result)
			err := result.err
			if err == nil {
				err = stageError(ExitRuntime, "fleet", fmt.Errorf("%s exited before all bots became online", result.stats.Name))
			}
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, err)
		case <-ctx.Done():
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, nil)
		}
	}

	if err := out.Emit("fleet_online", map[string]any{
		"online":   onlineCount,
		"count":    cfg.Count,
		"scenario": cfg.Scenario,
	}); err != nil {
		return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, stageError(ExitRuntime, "output", err))
	}

	for {
		select {
		case result := <-resultCh:
			results = append(results, result)
			err := result.err
			if err == nil {
				err = stageError(ExitRuntime, "fleet", fmt.Errorf("%s exited while the fleet was active", result.stats.Name))
			}
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, err)
		case <-ctx.Done():
			return finishFleet(ctx, cancelFleet, out, results, resultCh, launched, onlineCount, nil)
		}
	}
}

func finishFleet(
	ctx context.Context,
	cancel context.CancelFunc,
	out *output.Emitter,
	results []instanceResult,
	resultCh <-chan instanceResult,
	launched int,
	onlineCount int,
	firstErr error,
) error {
	cancel()
	for len(results) < launched {
		result := <-resultCh
		results = append(results, result)
		if firstErr == nil && result.err != nil {
			firstErr = result.err
		}
	}

	var packets uint64
	var chunks uint64
	var authInputs uint64
	var movementInputs uint64
	var actionPackets uint64
	var flyingConfirmed int
	var serverCorrections uint64
	minHorizontalDistance := -1.0
	for _, result := range results {
		stats := result.stats
		packets += stats.PacketsReceived
		chunks += stats.ChunksReceived
		authInputs += stats.AuthInputsSent
		movementInputs += stats.MovementInputsSent
		actionPackets += stats.ActionPacketsSent
		fields := map[string]any{
			"bot":                  stats.Name,
			"index":                stats.Index,
			"scenario":             stats.Scenario,
			"online":               !stats.OnlineAt.IsZero(),
			"packets_received":     stats.PacketsReceived,
			"chunks_received":      stats.ChunksReceived,
			"auth_inputs_sent":     stats.AuthInputsSent,
			"movement_inputs_sent": stats.MovementInputsSent,
			"action_packets_sent":  stats.ActionPacketsSent,
		}
		if stats.Scenario == ScenarioChunkFly {
			spanX, spanZ := stats.chunkSpan()
			fields["start_position"] = []float32{stats.StartPosition[0], stats.StartPosition[1], stats.StartPosition[2]}
			fields["final_position"] = []float32{stats.FinalPosition[0], stats.FinalPosition[1], stats.FinalPosition[2]}
			fields["horizontal_distance"] = stats.HorizontalDistance
			fields["flying_confirmed"] = stats.FlyingConfirmed
			fields["server_corrections"] = stats.ServerCorrections
			fields["chunk_span_x"] = spanX
			fields["chunk_span_z"] = spanZ
			serverCorrections += stats.ServerCorrections
			if stats.FlyingConfirmed {
				flyingConfirmed++
			}
			if minHorizontalDistance < 0 || stats.HorizontalDistance < minHorizontalDistance {
				minHorizontalDistance = stats.HorizontalDistance
			}
		}
		if !stats.EndedAt.IsZero() {
			fields["uptime"] = stats.EndedAt.Sub(stats.StartedAt).Round(time.Millisecond).String()
		}
		if result.err != nil {
			fields["error"] = result.err.Error()
		}
		if err := out.Emit("bot_stats", fields); err != nil && firstErr == nil {
			firstErr = stageError(ExitRuntime, "output", err)
		}
	}

	reason := "completed"
	if firstErr != nil {
		reason = "error"
	} else if ctx.Err() != nil {
		reason = "signal"
	}
	fields := map[string]any{
		"reason":               reason,
		"launched":             launched,
		"online":               onlineCount,
		"packets_received":     packets,
		"chunks_received":      chunks,
		"auth_inputs_sent":     authInputs,
		"movement_inputs_sent": movementInputs,
		"action_packets_sent":  actionPackets,
		"graceful_shutdown":    firstErr == nil,
	}
	if len(results) > 0 && results[0].stats.Scenario == ScenarioChunkFly {
		fields["flying_confirmed"] = flyingConfirmed
		fields["server_corrections"] = serverCorrections
		fields["min_horizontal_distance"] = minHorizontalDistance
	}
	if firstErr != nil {
		fields["error"] = firstErr.Error()
	}
	if err := out.Emit("fleet_shutdown", fields); err != nil && firstErr == nil {
		firstErr = stageError(ExitRuntime, "output", err)
	}
	return firstErr
}
