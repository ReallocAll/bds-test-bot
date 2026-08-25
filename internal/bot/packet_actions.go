package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/ReallocAll/bds-test-bot/internal/action"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

var ErrPacketWriterUnavailable = errors.New("packet writer unavailable")

const packetActionCooldownTicks = 5

// PacketAction emits one protocol packet and keeps the action alive briefly so
// Bedrock has time to process the packet before the scenario runner advances to
// the next protocol action. This avoids bursting stateful packets back-to-back.
type PacketAction struct {
	name          string
	writer        packetWriter
	build         func() packet.Packet
	sent          bool
	cooldownTicks uint64
}

func NewPacketAction(name string, writer packetWriter, build func() packet.Packet) *PacketAction {
	return &PacketAction{name: name, writer: writer, build: build}
}

func (a *PacketAction) Name() string { return a.name }

func (a *PacketAction) Start(context.Context) error { return nil }

func (a *PacketAction) Tick(context.Context, action.TickContext) error {
	if a.sent {
		if a.cooldownTicks > 0 {
			a.cooldownTicks--
		}
		return nil
	}
	if a.writer == nil {
		return ErrPacketWriterUnavailable
	}
	if err := a.writer.WritePacket(a.build()); err != nil {
		return err
	}
	a.sent = true
	a.cooldownTicks = packetActionCooldownTicks
	return nil
}

func (a *PacketAction) Done() bool { return a.sent && a.cooldownTicks == 0 }

func NewChatAction(writer packetWriter, sourceName, message string) *PacketAction {
	return NewPacketAction("chat", writer, func() packet.Packet {
		return &packet.Text{
			TextType:   packet.TextTypeChat,
			SourceName: sourceName,
			Message:    message,
		}
	})
}

func NewCommandAction(writer packetWriter, command string, playerUniqueID int64) *PacketAction {
	command = strings.TrimSpace(command)
	command = strings.TrimPrefix(command, "/")
	return NewPacketAction("command", writer, func() packet.Packet {
		return &packet.CommandRequest{
			CommandLine: command,
			CommandOrigin: protocol.CommandOrigin{
				Origin:         protocol.CommandOriginPlayer,
				UUID:           uuid.New(),
				PlayerUniqueID: playerUniqueID,
			},
			Internal: false,
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
