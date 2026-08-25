package bot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type recordingPacketWriter struct {
	packets []packet.Packet
	err     error
}

func (w *recordingPacketWriter) WritePacket(pk packet.Packet) error {
	if w.err != nil {
		return w.err
	}
	w.packets = append(w.packets, pk)
	return nil
}

func TestChatActionWritesTextPacket(t *testing.T) {
	writer := &recordingPacketWriter{}
	a := NewChatAction(writer, "LoadBot-01", "hello")
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Tick(context.Background(), action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	if !a.Done() || len(writer.packets) != 1 {
		t.Fatalf("done=%v packets=%d", a.Done(), len(writer.packets))
	}
	pk, ok := writer.packets[0].(*packet.Text)
	if !ok {
		t.Fatalf("packet = %T, want *packet.Text", writer.packets[0])
	}
	if pk.TextType != packet.TextTypeChat || pk.SourceName != "LoadBot-01" || pk.Message != "hello" {
		t.Fatalf("unexpected chat packet: %+v", pk)
	}
}

func TestCommandActionEncodesPlayerOrigin(t *testing.T) {
	writer := &recordingPacketWriter{}
	for tick := uint64(1); tick <= 2; tick++ {
		a := NewCommandAction(writer, "/list", 42)
		if err := a.Tick(context.Background(), action.TickContext{Tick: tick}); err != nil {
			t.Fatal(err)
		}
	}
	first, ok := writer.packets[0].(*packet.CommandRequest)
	if !ok {
		t.Fatalf("packet = %T, want *packet.CommandRequest", writer.packets[0])
	}
	second, ok := writer.packets[1].(*packet.CommandRequest)
	if !ok {
		t.Fatalf("packet = %T, want *packet.CommandRequest", writer.packets[1])
	}
	if first.CommandLine != "list" || first.CommandOrigin.Origin != protocol.CommandOriginPlayer || first.CommandOrigin.PlayerUniqueID != 42 {
		t.Fatalf("unexpected command packet: %+v", first)
	}
	if first.CommandOrigin.UUID == uuid.Nil || second.CommandOrigin.UUID == uuid.Nil {
		t.Fatal("command origin UUID must not be nil")
	}
	if first.CommandOrigin.UUID == second.CommandOrigin.UUID {
		t.Fatal("each command request must use a unique origin UUID")
	}
}

func TestSwingActionUsesRuntimeID(t *testing.T) {
	writer := &recordingPacketWriter{}
	a := NewSwingAction(writer, 42)
	if err := a.Tick(context.Background(), action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	pk, ok := writer.packets[0].(*packet.PlayerAction)
	if !ok {
		t.Fatalf("packet = %T, want *packet.PlayerAction", writer.packets[0])
	}
	if pk.EntityRuntimeID != 42 || pk.ActionType != protocol.PlayerActionMissedSwing {
		t.Fatalf("unexpected swing packet: %+v", pk)
	}
}

func TestPacketActionPropagatesWriterError(t *testing.T) {
	want := errors.New("write failed")
	writer := &recordingPacketWriter{err: want}
	a := NewChatAction(writer, "LoadBot", "hello")
	if err := a.Tick(context.Background(), action.TickContext{Tick: 1}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if a.Done() {
		t.Fatal("action must not complete after a failed write")
	}
}

func TestAuthInputWriterSeparatesActionPackets(t *testing.T) {
	base := &recordingPacketWriter{}
	var auth atomic.Uint64
	var movement atomic.Uint64
	var actions atomic.Uint64
	writer := authInputWriter{writer: base, authCount: &auth, movementCount: &movement, actionCount: &actions}

	state := newPlayerState(mgl32.Vec3{}, 0, 0)
	state.setMoveControl(mgl32.Vec2{0, 1}, chunkWalkStepPerTick, 0)
	if err := writer.WritePacket(authInputPacket(state, 1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.WritePacket(&packet.Text{TextType: packet.TextTypeChat, SourceName: "LoadBot", Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	if auth.Load() != 1 || movement.Load() != 1 || actions.Load() != 1 {
		t.Fatalf("auth=%d movement=%d actions=%d", auth.Load(), movement.Load(), actions.Load())
	}
}
