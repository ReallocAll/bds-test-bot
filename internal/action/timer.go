package action

import "context"

// Timed runs a callback for a fixed amount of ticks.
type Timed struct {
	ActionName string
	Ticks      uint64
	Current    uint64
	Callback   func(TickContext)
}

func NewTimed(name string, ticks uint64, callback func(TickContext)) *Timed {
	return &Timed{ActionName: name, Ticks: ticks, Callback: callback}
}

func (t *Timed) Name() string { return t.ActionName }

func (t *Timed) Start(context.Context) error {
	return nil
}

func (t *Timed) Tick(_ context.Context, tick TickContext) error {
	if t.Current < t.Ticks {
		if t.Callback != nil {
			t.Callback(tick)
		}
		t.Current++
	}
	return nil
}

func (t *Timed) Done() bool {
	return t.Current >= t.Ticks
}
