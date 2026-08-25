package bot

import (
	"context"
	"strings"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// PacketAction emits one protocol packet on its first tick and then completes.
type PacketAction struct {
	name   string
	writer packetWriter
	build  func() packet.Packet
	sent   bool
}

func NewPacketAction(name string, writer packetWriter, build func() packet.Packet) *PacketAction {
	return &PacketAction{name: name, writer: writer, build: build}
}

func (a *PacketAction) Name() string { return a.name }

func (a *PacketAction) Start(context.Context) error { return nil }

func (a *PacketAction) Tick(context.Context, action.TickContext) error {
	if a.sent {
		return nil
	}
	if a.writer == nil {
		return ErrPacketWriterUnavailable
	}
	if err := a.writer.WritePacket(a.build()); err != nil {
		return err
	}
	a.sent = true
	return nil
}

func (a *PacketAction) Done() bool { return a.sent }

func NewChatAction(writer packetWriter, sourceName, message string) *PacketAction {
	return NewPacketAction("chat", writer, func() packet.Packet {
		return &packet.Text{
			TextType:   packet.TextTypeChat,
			SourceName: sourceName,
			Message:    message,
		}
	})
}

func NewCommandAction(writer packetWriter, command string) *PacketAction {
	command = strings.TrimSpace(command)
	if !strings.HasPrefix(command, "/") {
		command = "/" + command
	}
	return NewPacketAction("command", writer, func() packet.Packet {
		return &packet.CommandRequest{
			CommandLine: command,
			CommandOrigin: protocol.CommandOrigin{
				Origin: protocol.CommandOriginPlayer,
			},
		}
	})
}

func NewSwingAction(writer packetWriter, entityRuntimeID uint64) *PacketAction {
	return NewPacketAction("swing", writer, func() packet.Packet {
		return &packet.PlayerAction{
			EntityRuntimeID: entityRuntimeID,
			ActionType:      protocol.PlayerActionMissedSwing,
		}
	})
}
