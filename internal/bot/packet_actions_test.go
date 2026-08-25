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
	if a.Done() || len(writer.packets) != 1 {
		t.Fatalf("done=%v packets=%d", a.Done(), len(writer.packets))
	}
	for tick := uint64(2); tick <= packetActionCooldownTicks; tick++ {
		if err := a.Tick(context.Background(), action.TickContext{Tick: tick}); err != nil {
			t.Fatal(err)
		}
		if a.Done() {
			t.Fatalf("action completed before cooldown elapsed at tick %d", tick)
		}
		if len(writer.packets) != 1 {
			t.Fatalf("packet was emitted more than once: %d", len(writer.packets))
		}
	}
	finalTick := packetActionCooldownTicks + 1
	if err := a.Tick(context.Background(), action.TickContext{Tick: finalTick}); err != nil {
		t.Fatal(err)
	}
	if !a.Done() {
		t.Fatal("action should complete after cooldown ticks")
	}
	if len(writer.packets) != 1 {
		t.Fatalf("packet was emitted more than once: %d", len(writer.packets))
	}
	pk, ok := writer.packets[0].(*packet.Text)
	if !ok || pk.TextType != packet.TextTypeChat || pk.SourceName != "LoadBot-01" || pk.Message != "hello" {
		t.Fatalf("unexpected chat packet: %+v", pk)
	}
}

func TestCommandActionEncodesPlayerOrigin(t *testing.T) {
	writer := &recordingPacketWriter{}
	a := NewCommandAction(writer, "/list", 42)
	if err := a.Tick(context.Background(), action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	pk := writer.packets[0].(*packet.CommandRequest)
	if pk.CommandLine != "list" || pk.CommandOrigin.Origin != protocol.CommandOriginPlayer || pk.CommandOrigin.PlayerUniqueID != 42 || pk.CommandOrigin.UUID == uuid.Nil {
		t.Fatalf("unexpected command packet: %+v", pk)
	}
}

func TestSwingActionUsesRuntimeID(t *testing.T) {
	writer := &recordingPacketWriter{}
	a := NewSwingAction(writer, 42)
	if err := a.Tick(context.Background(), action.TickContext{Tick: 1}); err != nil {
		t.Fatal(err)
	}
	pk := writer.packets[0].(*packet.PlayerAction)
	if pk.EntityRuntimeID != 42 || pk.ActionType != protocol.PlayerActionMissedSwing {
		t.Fatalf("unexpected swing packet: %+v", pk)
	}
}

func TestPacketActionPropagatesWriterError(t *testing.T) {
	want := errors.New("write failed")
	a := NewChatAction(&recordingPacketWriter{err: want}, "LoadBot", "hello")
	if err := a.Tick(context.Background(), action.TickContext{Tick: 1}); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	if a.Done() {
		t.Fatal("action must not complete after failed write")
	}
}

func TestAuthInputWriterSeparatesActionPackets(t *testing.T) {
	base := &recordingPacketWriter{}
	var auth, movement, actions atomic.Uint64
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
