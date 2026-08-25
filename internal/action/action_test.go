package action

import (
	"context"
	"testing"
)

func TestSequence(t *testing.T) {
	ctx := context.Background()
	count := 0
	s := NewSequence(NewTimed("first", 3, func(TickContext) { count++ }))
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for i := uint64(0); i < 3; i++ {
		if err := s.Tick(ctx, TickContext{Tick: i}); err != nil {
			t.Fatal(err)
		}
	}
	if !s.Done() {
		t.Fatal("sequence should be done")
	}
	if count != 3 {
		t.Fatalf("got %d callbacks", count)
	}
}
