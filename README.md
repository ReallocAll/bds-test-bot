# bds-test-bot

Minimal headless Minecraft Bedrock client for BDS integration testing.

## Current capabilities

- RakNet connection through `github.com/sandertv/gophertunnel`
- Login and spawn handshake
- Chunk radius request
- Chunk streaming observation
- JSON Lines events
- Graceful shutdown on SIGINT/SIGTERM

## Build

```bash
go build ./cmd/bds-test-bot
```

## Run

```bash
./bds-test-bot --host 127.0.0.1 --port 19132 --name TestBot
```

JSON output:

```bash
./bds-test-bot --json
```

## Test server

Recommended BDS integration test settings:

```properties
online-mode=false
allow-list=false
player-idle-timeout=0
view-distance=8
tick-distance=4
```

## Limitations

v0.1 is a protocol test client, not a gameplay bot. It does not implement movement, inventory, combat, or world simulation.
