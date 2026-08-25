package bot

import "testing"

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != DefaultHost || cfg.Port != DefaultPort || cfg.Name != DefaultName {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ChunkRadius != DefaultChunkRadius {
		t.Fatalf("chunk radius = %d, want %d", cfg.ChunkRadius, DefaultChunkRadius)
	}
	if cfg.ConnectTimeout != DefaultConnectTimeout || cfg.SpawnTimeout != DefaultSpawnTimeout {
		t.Fatalf("unexpected timeout defaults: %+v", cfg)
	}
}

func TestParseConfigRejectsInvalidPort(t *testing.T) {
	if _, err := ParseConfig([]string{"--port", "70000"}); err == nil {
		t.Fatal("expected invalid port error")
	}
}
