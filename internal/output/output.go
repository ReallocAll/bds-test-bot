package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type Emitter struct {
	mu       sync.Mutex
	out      io.Writer
	jsonMode bool
	now      func() time.Time
}

func New(out io.Writer, jsonMode bool) *Emitter {
	return &Emitter{out: out, jsonMode: jsonMode, now: time.Now}
}

func (e *Emitter) Emit(name string, fields map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.jsonMode {
		line := make(map[string]any, len(fields)+1)
		line["event"] = name
		for key, value := range fields {
			line[key] = value
		}
		encoded, err := json.Marshal(line)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(e.out, string(encoded))
		return err
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, fields[key]))
	}
	if len(parts) == 0 {
		_, err := fmt.Fprintf(e.out, "%s INFO %s\n", e.now().Format("15:04:05"), name)
		return err
	}
	_, err := fmt.Fprintf(e.out, "%s INFO %s %s\n", e.now().Format("15:04:05"), name, strings.Join(parts, " "))
	return err
}
