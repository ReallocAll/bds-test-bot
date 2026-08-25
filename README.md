# bds-test-bot

`bds-test-bot` is a minimal headless Minecraft Bedrock network client and fleet load generator for BDS and Endstone integration tests. It connects through the real Bedrock/RakNet protocol, completes the normal gophertunnel login/spawn sequence, requests chunks, sends an independent `PlayerAuthInput` stream at 20 ticks/s for every bot, and remains online until terminated.

It intentionally targets offline/LAN-style test servers. It is not a FakePlayer implementation, pathfinder, combat bot, or Xbox automation client.

## Protocol baseline

The bot uses Go 1.25 and `github.com/sandertv/gophertunnel v1.59.0`. That release reports Minecraft Bedrock `1.26.44` and protocol `2168`, matching stable BDS `1.26.44.3` used by the GitHub Actions integration lab.

The newer gophertunnel commit `7f058e5ddc393eaa0480dae338c5eee2feb323e6` targets Minecraft `1.26.45` / protocol `2169`; real BDS validation showed that protocol is newer than the current stable BDS and is rejected as `server outdated`, so the bot deliberately remains on v1.59.0 until stable BDS advances.

The bot leaves `minecraft.Dialer.TokenSource` unset and supplies offline identity data, so BDS must have online authentication disabled.

## Current capabilities

- RakNet/Bedrock connection and offline login.
- Normal gophertunnel resource-pack and spawn handshake.
- `--count` fleet generation with an independent connection and `IdentityData` per bot.
- Deterministic unique names through `--name-prefix`.
- Staggered fleet login through `--login-stagger`.
- Wait-for-all-online orchestration and aggregate fleet status.
- Configurable chunk-radius request, default `8`.
- `LevelChunk` reception with per-bot counters.
- Independent `PlayerAuthInput` at 20 ticks/s per bot.
- `idle` and `chunk-walk` scenarios.
- `chunk-walk` emits forward movement input and position/delta updates, with fleet headings distributed around 360 degrees.
- Server movement/teleport correction tracking.
- Per-bot packet, chunk, auth-input, and movement-input statistics.
- Human-readable logs or JSON Lines for CI.
- Connect and spawn/world timeouts.
- Bulk graceful SIGINT/SIGTERM shutdown with aggregate shutdown evidence.

A bot reaches `online` only after spawn completes, a `ChunkRadiusUpdated` packet is received, and at least one `LevelChunk` is received. A fleet reaches `fleet_online` only after every launched bot reaches that state.

## Build

```bash
go build ./cmd/bds-test-bot
```

GitHub Actions builds and tests Linux and Windows and publishes workflow artifacts for both platforms.

## Quick start

Single idle bot:

```bash
./bds-test-bot \
  --host 127.0.0.1 \
  --port 19132 \
  --count 1 \
  --name-prefix TestBot \
  --scenario idle \
  --chunk-radius 8
```

Twenty walking bots:

```bash
./bds-test-bot \
  --host 127.0.0.1 \
  --port 19132 \
  --count 20 \
  --name-prefix TestBot \
  --scenario chunk-walk \
  --login-stagger 250ms \
  --chunk-radius 8 \
  --json
```

Useful CI options:

```text
--json
--connect-timeout 15s
--spawn-timeout 30s
--login-stagger 250ms
```

Defaults are:

```text
host            127.0.0.1
port            19132
count           1
name prefix     TestBot
scenario        idle
chunk radius    8
login stagger   250ms
connect timeout 15s
spawn timeout   30s
```

`--name` remains available as a legacy alias for a single bot and may only be used with `--count=1`.

## Scenarios

### `idle`

Bots keep their server-corrected position and send a stationary `PlayerAuthInput` every client tick. This is the baseline connection/player load scenario.

### `chunk-walk`

Each bot continuously sends forward movement at 20 TPS. Fleet members receive different headings so they fan out rather than all walking along the same path. The bot records both total auth inputs and movement auth inputs so CI can prove the movement workload was actually emitted.

`chunk-walk` is a deterministic load scenario, not pathfinding: it does not decode terrain or plan around obstacles.

## JSONL events

With `--json`, stdout contains one JSON object per line. Fleet output includes events such as:

```json
{"count":5,"event":"fleet_starting","name_prefix":"TestBot","scenario":"idle"}
{"bot":"TestBot-01","event":"connecting","index":1,"scenario":"idle"}
{"bot":"TestBot-01","event":"online","index":1,"chunks_received":95,"auth_inputs_sent":1}
{"count":5,"event":"fleet_online","online":5,"scenario":"idle"}
{"bot":"TestBot-01","event":"bot_stats","auth_inputs_sent":1200,"movement_inputs_sent":0}
{"event":"fleet_shutdown","launched":5,"online":5,"graceful_shutdown":true}
```

Errors are also machine-readable:

```json
{"event":"error","message":"...","stage":"spawn"}
```

## Recommended BDS test profile

`bds-test-bot` does not modify `server.properties`. The test harness should configure BDS. A suitable profile is:

```properties
online-mode=false
allow-list=false
player-idle-timeout=0
max-players=30

gamemode=creative
force-gamemode=true
difficulty=peaceful
allow-cheats=true
default-player-permission-level=operator

view-distance=8
tick-distance=4
client-side-chunk-generation-enabled=false
```

The critical settings are `online-mode=false`, `player-idle-timeout=0`, and a `max-players` value large enough for the requested fleet.

## Exit codes

| Code | Meaning |
| ---: | --- |
| 0 | Normal termination, including SIGINT/SIGTERM |
| 1 | General runtime/network/fleet error after connection |
| 2 | Invalid CLI arguments |
| 3 | Connect/login failure or timeout |
| 4 | Spawn/world readiness failure or timeout |

## Current limitations

- Offline/LAN test mode only; no Microsoft/Xbox OAuth flow.
- No terrain-aware pathfinding, combat, inventory, block interaction, crafting, or world database.
- Chunk payloads are counted but not decoded into a world representation.
- `chunk-walk` is synthetic forward movement for load testing and does not navigate obstacles.
