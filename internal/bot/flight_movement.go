package bot

import "github.com/go-gl/mathgl/mgl32"

const chunkFlyAltitudeTolerance = float32(0.05)

// applyFlightMovement first reaches the server-derived safe flight altitude,
// then starts horizontal traversal. Server MovePlayer/CorrectPlayerMovePrediction
// updates replace s.position, so every following prediction resumes from the
// latest authoritative state rather than stale client-only coordinates.
func applyFlightMovement(s *playerState, snapshot *authInputSnapshot) {
	remaining := s.control.flightTargetY - s.position[1]
	if remaining > chunkFlyAltitudeTolerance {
		snapshot.moveVector = mgl32.Vec2{}
		step := s.control.verticalStep
		if step <= 0 || step > remaining {
			step = remaining
		}
		snapshot.verticalDirection = 1
		snapshot.delta[1] = step
		s.position[1] += step
		snapshot.committedDelta[1] = step
		return
	}
	if remaining < -chunkFlyAltitudeTolerance {
		snapshot.moveVector = mgl32.Vec2{}
		step := s.control.verticalStep
		if step <= 0 || step > -remaining {
			step = -remaining
		}
		snapshot.verticalDirection = -1
		snapshot.delta[1] = -step
		s.position[1] -= step
		snapshot.committedDelta[1] = -step
		return
	}
	applyHorizontalMovement(s, snapshot, s.control.moveStep)
}
