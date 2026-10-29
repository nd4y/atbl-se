# API and device protocol

[Русский](../ru/api.md) | [Configuration](configuration.md)

The core exposes a local panel API and a compatible control API for external applications. The [machine-readable contract](../contracts/vacuglide2-control-api.json) lists all 26 compatible operations and their acceptance status. The target is stock VacuGlide 2 firmware `1.03`/`1.0.3`. Accounts, video hosting and OTA are outside this API.

## Authentication and client setup

| Interface               | Authentication                      | Purpose                                                    |
| ----------------------- | ----------------------------------- | ---------------------------------------------------------- |
| `/health`, panel assets | None                                | Version/availability and static UI; no control credentials |
| `/api/*`                | `Authorization: Bearer <local-key>` | Panel and local administration                             |
| Compatible HTTP routes  | `x-device-token: <local-key>`       | Independent local access, including offline status         |
| Compatible HTTP routes  | `x-device-token: <device-token>`    | Valid only while that device is connected                  |
| `/events/stream`        | `deviceToken` query parameter       | SSE with the same local-key/device-token rules             |

The local key is `dataDir/control-token`. The firmware token is held only as a hash in memory; disconnect revokes its access and device-bound event streams. Do not include a query URL containing a key in logs or issue reports.

In a client such as VacuGlide Studio, select the local server address and local key where its connection settings permit it. An application hard-coded to a cloud URL needs its own local-endpoint support or a tested network configuration. The bundled Studio adapter fixture verifies HTTP behavior; it does not prove every installed Studio version works unchanged. HTTP and HTTPS contracts are tested. HTTPS clients must trust the configured certificate; keep certificate verification enabled.

Pair the connected MAC in the panel. Set `compatibilityControl: true` for external-client writes, and enable the relevant script flag for script operations. Defaults permit status inspection and bounded panel control after pairing. External clients own their STOP behavior: their runs do not use the panel's two-second lease. Authenticate STOP even when playback flags are disabled.

## Local panel API

| Method and path       | Input / result                                                                            |
| --------------------- | ----------------------------------------------------------------------------------------- |
| `GET /api/status`     | Connection, pairing, current `generation`, limits and control status                      |
| `GET /api/state`      | Query state from the connected device                                                     |
| `POST /api/pair`      | `{"mac":"020000000001"}` for the synthetic device; use the actual connected MAC otherwise |
| `POST /api/start`     | `{"speed":15,"seconds":60,"generation":1}`; use the current generation, not a constant    |
| `POST /api/keepalive` | `{"controlEpoch":"<value returned by start>"}`                                            |
| `POST /api/stop`      | Safe STOP: invalidate pending work, pause the motor and close both valves                 |
| `POST /api/shutdown`  | Available only with `allowShutdown: true`; safe stop and server shutdown                  |

Start accepts integer speed `1..maxSpeed` and duration `1..3600` seconds. The panel renews the returned epoch every 400 ms. Renewal cannot extend the original duration. A stale connection generation or expired/replaced epoch cannot resume a run. The two-second lease triggers a stop attempt when panel renewal ends; it cannot guarantee a physical stop after the device link is lost.

`stopConfirmed` means firmware acknowledgement, not a physical sensor measurement. A health response confirms the server version, not device connection. Read connection status separately.

## Compatible operations

| Group           | Routes and request fields                                                                                                                                    |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Discovery/state | `GET /vacuglide/info`, `/vacuglide/state`, `/vacuglide/connected`, `/autoblow/connected`, `/cluster/time`                                                    |
| Manual speed    | `PUT /vacuglide/target-speed` with `targetSpeed`; `PUT /vacuglide/target-speed/stop`                                                                         |
| Valves          | `PUT /vacuglide/valve/stroke-plus` or `/stroke-minus` with boolean `valveState`                                                                              |
| Native scripts  | `PUT /vacuglide/local-script` with `localScriptIndex`; suffixes `/start`, `/stop`; `GET /vacuglide/local-script/loaded`                                      |
| Slot changes    | `PUT /vacuglide/local-script/replace`, `/swap`, `/move`                                                                                                      |
| Sync upload     | `PUT /vacuglide/sync-script/upload-funscript`, `/upload-csv`, `/upload-funscript-url`, `/upload-csv-url`                                                     |
| Sync playback   | `PUT /vacuglide/sync-script/load-token` with `scriptToken`; `/start` with `startTimeMs`; `/stop`; `/offset` with `offsetTimeMs`; `/loop` with boolean `loop` |
| Events          | `GET /events/stream?deviceToken=<key>`                                                                                                                       |

For the manual compatible STOP, send an empty body and omit `Content-Type: application/json`. Sending an empty JSON document header or `{}` changes the official API validation path. Prefer the pinned SDK's request shapes for uploads and slot changes; [integration fixtures](../../scripts/client-contracts.mjs) exercise the complete sequences and edge cases.

Manual speed is `0..100`, bounded by `maxSpeed`. Native script activation requires `experimentalLocalScripts` and `maxSpeed: 100` because stored firmware scripts may use the full range. Sync scripts require `experimentalSyncScripts`; their peak speed must fit `maxSpeed`. The read-looking `/local-script/loaded` operation follows firmware behavior and switches to local-script paused mode. Slot replacement changes persistent device content; an imported local catalog file alone does not send a device command.

URL imports require an explicit `scriptURLOrigins` entry. No redirects or client authentication headers are forwarded. See [configuration](configuration.md) for address and size restrictions. The server stores converted sync scripts locally and serves a bounded device transfer; it does not need a cloud download at playback time. [Data and tools](data.md) describes importing native catalog files.

Failures distinguish validation/authentication, disabled features, disconnected devices, admission limits and device timeouts. Check the returned status and body. Do not retry state-changing operations blindly after a timeout: first read back the current state. Command replies are correlated with the active connection; a successful synthetic reply is not hardware evidence.

## Wire format and valves

The device connects to `/device-backend/ws` over TLS, normally through `latency.autoblowapi.com` on port 443. Header validation checks hardware, supported firmware, MAC and token. The local DNS/routing setup delivers this connection to the core without changing firmware.

```text
/target-speed-set/15/<uuid>
/target-speed-stop/<uuid>
/valve-stroke-plus-set/1/<uuid>
/valve-stroke-minus-set/0/<uuid>
/state/<mode>/<slot>/<speed>/<plus>/<minus>/<time>/<offset>/<loop>/<token>/<uuid>
```

Modes 43/44 mean manual playing/paused, 41/42 native-script playing/paused, and 60/70 sync-script playing/paused. A paused device can retain its speed setpoint. Compatible script STOP can retain valve state; the panel's safe STOP closes both valves separately.

Native files have an eight-byte little-endian header (script ID, marker 1), followed by six-byte records: `uint32 milliseconds`, `uint8 opcode`, `uint8 value`. Opcode 0 sets speed, 1 controls stroke-minus and 2 controls stroke-plus. Keep event order and duplicate timestamps. The offline decoder preserves unknown records; the server import validator accepts only supported opcodes and validates ordering, time and values. Funscript positions convert to speed and cannot reconstruct valve events. Preserve native binary records when valve timing matters.
