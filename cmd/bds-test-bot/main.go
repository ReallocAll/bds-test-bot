package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type event struct {
	Event string `json:"event"`
	X int32 `json:"x,omitempty"`
	Z int32 `json:"z,omitempty"`
	Total int `json:"total,omitempty"`
	Message string `json:"message,omitempty"`
}

func emit(jsonMode bool, e event) {
	if jsonMode {
		b, _ := json.Marshal(e)
		fmt.Println(string(b))
		return
	}
	log.Printf("%s x=%d z=%d total=%d %s", e.Event, e.X, e.Z, e.Total, e.Message)
}

func main() {
	host := flag.String("host", "127.0.0.1", "BDS host")
	port := flag.Int("port", 19132, "BDS port")
	name := flag.String("name", "TestBot", "bot name")
	radius := flag.Uint32("chunk-radius", 8, "requested chunk radius")
	jsonMode := flag.Bool("json", false, "JSONL output")
	flag.Parse()

	address := fmt.Sprintf("%s:%d", *host, *port)
	_ = name // gophertunnel creates default client data; name customization follows in a later version.
	emit(*jsonMode, event{Event: "connecting", Message: address})

	conn, err := minecraft.DialTimeout("raknet", address, 15*time.Second)
	if err != nil {
		emit(*jsonMode, event{Event: "error", Message: err.Error()})
		os.Exit(3)
	}
	defer conn.Close()
	emit(*jsonMode, event{Event: "connected"})

	if err := conn.DoSpawn(); err != nil {
		emit(*jsonMode, event{Event: "error", Message: err.Error()})
		os.Exit(4)
	}
	emit(*jsonMode, event{Event: "spawned"})

	if err := conn.WritePacket(&packet.RequestChunkRadius{ChunkRadius: *radius, MaxChunkRadius: *radius}); err != nil {
		emit(*jsonMode, event{Event: "error", Message: err.Error()})
		os.Exit(1)
	}

	chunks := 0
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		_ = conn.Close()
	}()

	emit(*jsonMode, event{Event: "online"})
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			return
		}
		switch p := pk.(type) {
		case *packet.LevelChunk:
			chunks++
			emit(*jsonMode, event{Event: "chunk_received", X: p.Position[0], Z: p.Position[1], Total: chunks})
		}
	}
}
