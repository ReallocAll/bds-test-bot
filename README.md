# bds-test-bot

`bds-test-bot` is a minimal headless Minecraft Bedrock network client for BDS and Endstone integration tests. It connects through the real Bedrock/RakNet protocol, completes the normal gophertunnel login/spawn sequence, requests chunks, sends a stationary `PlayerAuthInput` every client tick, and remains online until terminated.

v0.1 intentionally targets offline/LAN-style test servers. It is not a FakePlayer implementation, gameplay bot, pathfinder, or Xbox automation client.

## Protocol baseline

v0.1 uses Go 1.25 and `github.com/sandertv/gophertunnel v1.59.0`. That release reports Minecraft Bedrock `1.26.44` and protocol `2168`, matching the current stable Linux BDS downloaded by the Endstone bootstrap on 2026-08-25 (`1.26.44.3`).

The newer gophertunnel commit `7f058e5ddc393eaa0480dae338c5eee2feb323e6` targets Minecraft `1.26.45` / protocol `2169`; real BDS validation showed that protocol is newer than the current stable BDS and is rejected as `server outdated`, so v0.1 deliberately remains on v1.59.0 until stable BDS advances.

The bot leaves `minecraft.Dialer.TokenSource` unset and supplies only offline identity data, so BDS must have online authentication disabled.

## Current capabilities

- RakNet/Bedrock connection and offline login.
- Normal gophertunnel resource-pack and spawn handshake.
- Configurable chunk-radius request, default `8`.
- `LevelChunk` reception with counters.
- Stationary `PlayerAuthInput` at 20 ticks/s using the current gophertunnel packet layout.
- Server movement/teleport correction tracking.
- Human-readable logs or JSON Lines for CI.
- Connect and spawn/world timeouts.
- Clean SIGINT/SIGTERM shutdown.

`online` is emitted only after spawn completed, a `ChunkRadiusUpdated` packet was received, and at least one `LevelChunk` was received.

## Build

```bash
go build ./cmd/bds-test-bot
```

GitHub Actions builds and tests Linux and Windows and publishes these workflow artifacts:

- `bds-test-bot-linux-amd64`
- `bds-test-bot-windows-amd64`

## Quick start

```bash
./bds-test-bot \
  --host 127.0.0.1 \
  --port 19132 \
  --name TestBot \
  --chunk-radius 8
```

Useful CI options:

```text
--json
--connect-timeout 15s
--spawn-timeout 30s
```

Defaults are:

```text
host            127.0.0.1
port            19132
name            TestBot
chunk radius    8
connect timeout 15s
spawn timeout   30s
```

## JSONL events

With `--json`, stdout contains one JSON object per line. Typical startup output is:

```json
{"address":"127.0.0.1:19132","event":"connecting","name":"TestBot"}
{"event":"connected"}
{"event":"start_game"}
{"event":"spawned","x":0.5,"y":64,"z":0.5}
{"event":"chunk_radius_requested","radius":8}
{"event":"chunk_radius","radius":8}
{"event":"chunk_received","total":1,"x":0,"z":0}
{"chunks_received":1,"event":"online","packets_received":12,"uptime":"1.2s"}
```

Errors are also machine-readable:

```json
{"event":"error","message":"...","stage":"spawn"}
```

## Recommended BDS test profile

`bds-test-bot` does not modify `server.properties`. The test harness should configure BDS. A suitable v0.1 profile is:

```properties
online-mode=false
allow-list=false
player-idle-timeout=0

gamemode=creative
force-gamemode=true
difficulty=peaceful
allow-cheats=true
default-player-permission-level=operator

view-distance=8
tick-distance=4
client-side-chunk-generation-enabled=false
```

The critical settings for the MVP are `online-mode=false` and `player-idle-timeout=0`.

## Exit codes

| Code | Meaning |
| ---: | --- |
| 0 | Normal termination, including SIGINT/SIGTERM |
| 1 | General runtime/network error after connection |
| 2 | Invalid CLI arguments |
| 3 | Connect/login failure or timeout |
| 4 | Spawn/world readiness failure or timeout |

## Current limitations

- Offline/LAN test mode only; no Microsoft/Xbox OAuth flow in v0.1.
- No movement, pathfinding, combat, inventory, block interaction, crafting, or world database.
- Chunk payloads are not decoded into a world representation.
- One bot process represents one player; multi-bot load generation is out of scope for v0.1.
