package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != DefaultHost || cfg.Port != DefaultPort || cfg.NamePrefix != DefaultNamePrefix {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Count != DefaultCount || cfg.Scenario != ScenarioIdle {
		t.Fatalf("unexpected fleet defaults: %+v", cfg)
	}
	if cfg.ChunkRadius != DefaultChunkRadius {
		t.Fatalf("chunk radius = %d, want %d", cfg.ChunkRadius, DefaultChunkRadius)
	}
	if cfg.ConnectTimeout != DefaultConnectTimeout || cfg.SpawnTimeout != DefaultSpawnTimeout {
		t.Fatalf("unexpected timeout defaults: %+v", cfg)
	}
	if cfg.LoginStagger != DefaultLoginStagger {
		t.Fatalf("login stagger = %s, want %s", cfg.LoginStagger, DefaultLoginStagger)
	}
	if got := instanceName(cfg, 0); got != DefaultNamePrefix {
		t.Fatalf("single instance name = %q, want %q", got, DefaultNamePrefix)
	}
}

func TestParseConfigFleetNames(t *testing.T) {
	cfg, err := ParseConfig([]string{"--count", "20", "--name-prefix", "LoadBot", "--scenario", "idle"})
	if err != nil {
		t.Fatal(err)
	}
	if got := instanceName(cfg, 0); got != "LoadBot-01" {
		t.Fatalf("first fleet name = %q", got)
	}
	if got := instanceName(cfg, 19); got != "LoadBot-20" {
		t.Fatalf("last fleet name = %q", got)
	}
}

func TestParseConfigLegacyName(t *testing.T) {
	cfg, err := ParseConfig([]string{"--name", "LegacyBot"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NamePrefix != "LegacyBot" || instanceName(cfg, 0) != "LegacyBot" {
		t.Fatalf("legacy name not preserved: %+v", cfg)
	}
}

func TestParseConfigAcceptsChunkWalk(t *testing.T) {
	cfg, err := ParseConfig([]string{"--scenario", ScenarioChunkWalk})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scenario != ScenarioChunkWalk {
		t.Fatalf("scenario = %q, want %q", cfg.Scenario, ScenarioChunkWalk)
	}
}

func TestParseConfigAcceptsChunkFly(t *testing.T) {
	cfg, err := ParseConfig([]string{"--scenario", ScenarioChunkFly, "--count", "20"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scenario != ScenarioChunkFly || cfg.Count != 20 {
		t.Fatalf("unexpected chunk-fly config: %+v", cfg)
	}
}

func TestParseConfigLoadsScenarioFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.yaml")
	data := []byte("name: mixed\nsteps:\n  - action: move\n    ticks: 4\n    forward: 1\n  - action: wait\n    ticks: 2\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig([]string{"--scenario-file", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scenario != "mixed" || cfg.ScenarioDefinition == nil || len(cfg.ScenarioDefinition.Steps) != 2 {
		t.Fatalf("scenario file not loaded: %+v", cfg)
	}
}

func TestParseConfigRejectsInvalidPort(t *testing.T) {
	if _, err := ParseConfig([]string{"--port", "70000"}); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestParseConfigRejectsLegacyNameForFleet(t *testing.T) {
	if _, err := ParseConfig([]string{"--count", "5", "--name", "Bot"}); err == nil {
		t.Fatal("expected --name fleet error")
	}
}

func TestParseConfigRejectsUnsupportedScenario(t *testing.T) {
	if _, err := ParseConfig([]string{"--scenario", "combat"}); err == nil {
		t.Fatal("expected unsupported scenario error")
	}
}
