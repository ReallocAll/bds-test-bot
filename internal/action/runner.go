package action

import "context"

type Runner struct {
	action Action
}

func NewRunner(a Action) *Runner {
	return &Runner{action: a}
}

func (r *Runner) Start(ctx context.Context) error {
	if r.action == nil {
		return nil
	}
	return r.action.Start(ctx)
}

func (r *Runner) Tick(ctx context.Context, tick TickContext) error {
	if r.action == nil || r.action.Done() {
		return nil
	}
	return r.action.Tick(ctx, tick)
}
