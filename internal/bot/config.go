package bot

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	scenarioengine "github.com/ReallocAll/bds-test-bot/internal/scenario"
)

const (
	DefaultHost        = "127.0.0.1"
	DefaultPort        = 19132
	DefaultNamePrefix  = "TestBot"
	DefaultCount       = 1
	DefaultChunkRadius = 8
	ScenarioIdle       = "idle"
	ScenarioChunkWalk  = "chunk-walk"
	ScenarioChunkFly   = "chunk-fly"
)

var (
	DefaultConnectTimeout = 15 * time.Second
	DefaultSpawnTimeout   = 30 * time.Second
	DefaultLoginStagger   = 250 * time.Millisecond
)

type Config struct {
	Host               string
	Port               int
	NamePrefix         string
	Count              int
	Scenario           string
	ScenarioFile       string
	ScenarioDefinition *scenarioengine.Scenario
	ChunkRadius        int32
	JSON               bool
	ConnectTimeout     time.Duration
	SpawnTimeout       time.Duration
	LoginStagger       time.Duration
}

func DefaultConfig() Config {
	return Config{
		Host:           DefaultHost,
		Port:           DefaultPort,
		NamePrefix:     DefaultNamePrefix,
		Count:          DefaultCount,
		Scenario:       ScenarioIdle,
		ChunkRadius:    DefaultChunkRadius,
		ConnectTimeout: DefaultConnectTimeout,
		SpawnTimeout:   DefaultSpawnTimeout,
		LoginStagger:   DefaultLoginStagger,
	}
}

func ParseConfig(args []string) (Config, error) {
	cfg := DefaultConfig()
	fs := flag.NewFlagSet("bds-test-bot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var radius int
	var legacyName string
	fs.StringVar(&cfg.Host, "host", cfg.Host, "BDS host")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "BDS port")
	fs.StringVar(&legacyName, "name", "", "single offline test player name (legacy alias)")
	fs.StringVar(&cfg.NamePrefix, "name-prefix", cfg.NamePrefix, "offline test player name prefix")
	fs.IntVar(&cfg.Count, "count", cfg.Count, "number of independent bot connections")
	fs.StringVar(&cfg.Scenario, "scenario", cfg.Scenario, "built-in load scenario")
	fs.StringVar(&cfg.ScenarioFile, "scenario-file", "", "JSON or YAML scenario definition")
	fs.IntVar(&radius, "chunk-radius", int(cfg.ChunkRadius), "requested chunk radius")
	fs.BoolVar(&cfg.JSON, "json", false, "emit JSON Lines on stdout")
	fs.DurationVar(&cfg.ConnectTimeout, "connect-timeout", cfg.ConnectTimeout, "connect/login timeout")
	fs.DurationVar(&cfg.SpawnTimeout, "spawn-timeout", cfg.SpawnTimeout, "spawn/world timeout")
	fs.DurationVar(&cfg.LoginStagger, "login-stagger", cfg.LoginStagger, "delay between fleet login attempts")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if cfg.Host == "" {
		return Config{}, errors.New("host must not be empty")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("port must be in range 1..65535: %d", cfg.Port)
	}
	if cfg.Count < 1 || cfg.Count > 100 {
		return Config{}, fmt.Errorf("count must be in range 1..100: %d", cfg.Count)
	}
	if legacyName != "" {
		if cfg.Count != 1 {
			return Config{}, errors.New("--name may only be used with --count=1; use --name-prefix for fleets")
		}
		cfg.NamePrefix = legacyName
	}
	cfg.NamePrefix = strings.TrimSpace(cfg.NamePrefix)
	if cfg.NamePrefix == "" {
		return Config{}, errors.New("name-prefix must not be empty")
	}

	cfg.ScenarioFile = strings.TrimSpace(cfg.ScenarioFile)
	if cfg.ScenarioFile != "" {
		definition, err := scenarioengine.Load(cfg.ScenarioFile)
		if err != nil {
			return Config{}, err
		}
		cfg.ScenarioDefinition = &definition
		cfg.Scenario = definition.Name
	} else {
		switch cfg.Scenario {
		case ScenarioIdle, ScenarioChunkWalk, ScenarioChunkFly:
		default:
			return Config{}, fmt.Errorf("unsupported scenario %q (supported built-ins: %s, %s, %s; or use --scenario-file)", cfg.Scenario, ScenarioIdle, ScenarioChunkWalk, ScenarioChunkFly)
		}
	}

	if radius < 1 || radius > 96 {
		return Config{}, fmt.Errorf("chunk-radius must be in range 1..96: %d", radius)
	}
	cfg.ChunkRadius = int32(radius)
	if cfg.ConnectTimeout <= 0 {
		return Config{}, errors.New("connect-timeout must be greater than zero")
	}
	if cfg.SpawnTimeout <= 0 {
		return Config{}, errors.New("spawn-timeout must be greater than zero")
	}
	if cfg.LoginStagger < 0 {
		return Config{}, errors.New("login-stagger must not be negative")
	}
	for i := 0; i < cfg.Count; i++ {
		name := instanceName(cfg, i)
		if utf8.RuneCountInString(name) > 16 {
			return Config{}, fmt.Errorf("generated player name %q exceeds 16 characters", name)
		}
	}
	return cfg, nil
}

func instanceName(cfg Config, index int) string {
	if cfg.Count == 1 {
		return cfg.NamePrefix
	}
	width := len(strconv.Itoa(cfg.Count))
	if width < 2 {
		width = 2
	}
	return fmt.Sprintf("%s-%0*d", cfg.NamePrefix, width, index+1)
}
