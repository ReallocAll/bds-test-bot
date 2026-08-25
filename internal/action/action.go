package action

import "context"

// Action is a deterministic bot behavior unit executed over time.
// Implementations should be safe to run independently for each bot.
type Action interface {
	Name() string
	Start(context.Context) error
	Tick(context.Context, TickContext) error
	Done() bool
}

// TickContext contains the current simulated client tick.
type TickContext struct {
	Tick uint64
}

// Sequence executes actions in order.
type Sequence struct {
	actions []Action
	index   int
}

func NewSequence(actions ...Action) *Sequence {
	return &Sequence{actions: actions}
}

func (s *Sequence) Name() string { return "sequence" }

func (s *Sequence) Start(ctx context.Context) error {
	if len(s.actions) == 0 {
		return nil
	}
	return s.actions[0].Start(ctx)
}

func (s *Sequence) Tick(ctx context.Context, tick TickContext) error {
	if s.index >= len(s.actions) {
		return nil
	}
	current := s.actions[s.index]
	if err := current.Tick(ctx, tick); err != nil {
		return err
	}
	if current.Done() {
		s.index++
		if s.index < len(s.actions) {
			return s.actions[s.index].Start(ctx)
		}
	}
	return nil
}

func (s *Sequence) Done() bool {
	return s.index >= len(s.actions)
}
