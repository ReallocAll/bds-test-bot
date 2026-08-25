package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONLinesOutput(t *testing.T) {
	var buf bytes.Buffer
	emitter := New(&buf, true)
	if err := emitter.Emit("online", map[string]any{"chunks_received": 1}); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(buf.String())
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("invalid JSONL: %v", err)
	}
	if got["event"] != "online" {
		t.Fatalf("event = %v, want online", got["event"])
	}
	if got["chunks_received"] != float64(1) {
		t.Fatalf("chunks_received = %v, want 1", got["chunks_received"])
	}
}
