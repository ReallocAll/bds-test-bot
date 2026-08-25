package bot

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

const (
	DefaultHost        = "127.0.0.1"
	DefaultPort        = 19132
	DefaultName        = "TestBot"
	DefaultChunkRadius = 8
)

var (
	DefaultConnectTimeout = 15 * time.Second
	DefaultSpawnTimeout   = 30 * time.Second
)

type Config struct {
	Host           string
	Port           int
	Name           string
	ChunkRadius    int32
	JSON           bool
	ConnectTimeout time.Duration
	SpawnTimeout   time.Duration
}

func DefaultConfig() Config {
	return Config{
		Host:           DefaultHost,
		Port:           DefaultPort,
		Name:           DefaultName,
		ChunkRadius:    DefaultChunkRadius,
		ConnectTimeout: DefaultConnectTimeout,
		SpawnTimeout:   DefaultSpawnTimeout,
	}
}

func ParseConfig(args []string) (Config, error) {
	cfg := DefaultConfig()
	fs := flag.NewFlagSet("bds-test-bot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var radius int
	fs.StringVar(&cfg.Host, "host", cfg.Host, "BDS host")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "BDS port")
	fs.StringVar(&cfg.Name, "name", cfg.Name, "offline test player name")
	fs.IntVar(&radius, "chunk-radius", int(cfg.ChunkRadius), "requested chunk radius")
	fs.BoolVar(&cfg.JSON, "json", false, "emit JSON Lines on stdout")
	fs.DurationVar(&cfg.ConnectTimeout, "connect-timeout", cfg.ConnectTimeout, "connect/login timeout")
	fs.DurationVar(&cfg.SpawnTimeout, "spawn-timeout", cfg.SpawnTimeout, "spawn/world timeout")
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
	if cfg.Name == "" {
		return Config{}, errors.New("name must not be empty")
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
	return cfg, nil
}
