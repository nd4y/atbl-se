# Installation, control and development

[Русский](../ru/guide.md) | [README](../../README.md)

All project-owned commands, binaries, packages and environment variables now use `atblse`. See the [migration and recovery guide](migration.md) before replacing an earlier build. Official API routes, firmware identifiers and upstream SDK names remain unchanged.

## Installation and network

The runtime has no cloud dependency. Preload binaries, Docker images and configuration before disconnecting WAN. Do not expose device or control ports to the internet. Limit DNS/NAT changes to the intended device and keep an independent way back to the router.

### Windows

Build the two entry points and keep `windows/` next to them:

```sh
GOOS=windows GOARCH=amd64 go build -o out/windows/atblse-server.exe ./cmd/atblse-server
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o out/windows/atblse.exe ./cmd/atblse-windows
```

Copy `windows/` and `docs/` into `out/windows/`; launch `atblse.exe`. The wrapper uses Windows PowerShell 5.1, .NET Framework and WinRT. It does not need Python, WSL or Docker Desktop. Select the autonomous access point or the router-assisted LAN mode. A hotspot needs a compatible Wi-Fi Direct adapter; initial provisioning and DHCP/DNS need physical acceptance. Connect the device to the displayed Wi-Fi through its normal setup. Returning to the original network requires its normal provisioning again.

The launcher copies and verifies legacy data before publishing the new directory. Data and hotspot credentials live under `%LOCALAPPDATA%/atblse`. Do not publish that directory. Optional `control-options.json` uses the server fields `compatibilityControl`, `experimentalSyncScripts`, `experimentalLocalScripts`, `maxSpeed` and `scriptURLOrigins`. Stop the application before editing. Successful shutdown first verifies the owned server process, then removes its hotspot. If process exit cannot be confirmed, the wrapper retains the connection and exposes STOP for recovery.

### NAS Docker

Build the Linux server into an isolated image context with `deploy/Dockerfile` and `/etc/ssl/certs/ca-certificates.crt`, then build/tag `atblse:0.2.0-rc.3`. The scratch image contains the executable and CA roots. Save it with `docker save` and transfer the archive for an offline `docker load`.

Set `ATBLSE_DATA_DIR` and `ATBLSE_CONFIG_DIR` to private absolute directories. Copy `deploy/server.example.json` to the latter as `server.json`. UID/GID 65532 needs write access to data and read access to config. `deploy/compose.yaml` uses `pull_policy: never`, `restart: no`, read-only root, 96 MiB and no additional capabilities. It maps host TLS port 20445 to container 20443 and API 20881 to 20880. Nothing is installed or started by publishing this repository.

### Router and DNS choices

Example addresses below are placeholders: router `192.168.50.1`, server `192.168.50.10`, device `192.168.50.60`.

1. **Device-specific DHCP DNS.** On OpenWrt/dnsmasq assign this device a tag and DHCP option 6 pointing at a dedicated DNS resolver. Use a local wildcard zone for `autoblowapi.com`. The device must reach the server on TCP 443. Keep the setting limited to its MAC/reservation.
2. **Router, Pi-hole or AdGuard overrides.** Override `latency.autoblowapi.com` and the selected regional host, for example `eu-central-1.autoblowapi.com`. Prefer a supported local wildcard zone; do not leave a competing public AAAA record. Shared overrides affect all clients using that resolver, including other Autoblow devices.
3. **NAS with occupied port 443.** Return the router LAN address from the device-specific DNS. DNAT only the device's TCP 443 to server port 20445 and DNS UDP/TCP 53 to the chosen local resolver port. Use matching hairpin SNAT so replies return through the router. A DNS record cannot select a port, and same-subnet traffic sent straight to the NAS bypasses router DNAT.

These patterns apply to OpenWrt and MikroTik with their respective DHCP/firewall syntax; inspect actual rule matches and counters. For rollback, stop control, restore only the changed DHCP/DNS/NAT entries and reconnect the device. Do not flush global connection tracking or DNS configuration. Test WAN loss on a dedicated network or with a device-specific deny rule. Physical Go-server acceptance of these paths is pending.

## API and protocol

The device uses `wss://latency.autoblowapi.com/device-backend/ws`, TCP 443. Supported headers identify hardware `vacuglide`, firmware `1.03` or `1.0.3`, MAC and a device token. The server compares the device token as a memory-only SHA-256 hash. Panel authentication uses its separate generated `dataDir/control-token`.

All `/api/*` requests require `Authorization: Bearer <local-key>`. `/health` and static assets are public without revealing device credentials. Pair with `POST /api/pair` and `{"mac":"020000000001"}` for the simulator. Read `/api/status` to obtain the current `generation`; `POST /api/start` accepts `{"speed":15,"seconds":60,"generation":1}`. It returns `controlEpoch`; renew through `/api/keepalive` every 400 ms. The two-second lease and original duration are both enforced. `/api/stop` invalidates pending work and confirms motor pause plus closed valves from protocol replies.

Compatible `/vacuglide/*` and `/autoblow/*` routes use `x-device-token`, accept the local key, and require pairing plus `compatibilityControl` for writes. They intentionally use client-owned STOP semantics rather than the panel lease. Enable `experimentalSyncScripts` and/or `experimentalLocalScripts` for those features; playback requires `maxSpeed: 100` because native scripts can contain full-range speed. URL imports require an explicit `scriptURLOrigins` allowlist. Use an empty list to forbid outbound script retrieval.

The [machine-readable contract](../contracts/vacuglide2-control-api.json) lists the implemented routes and explicit policy differences. Replies are validated and correlated; no runtime success is invented without a device reply. The virtual client deliberately produces synthetic replies.

```text
/target-speed-set/15/<uuid>
/target-speed-stop/<uuid>
/valve-stroke-plus-set/1/<uuid>
/valve-stroke-minus-set/0/<uuid>
/state/<mode>/<slot>/<speed>/<plus>/<minus>/<time>/<offset>/<loop>/<token>/<uuid>
```

Modes 43/44 are manual playing/paused, 41/42 local-script playing/paused, and 60/70 sync-script playing/paused. STOP can retain the last speed setpoint. A nonzero speed is not proof of motion. Native script pause can leave valves unchanged; the panel's safe STOP separately closes them. `stopConfirmed` records firmware acknowledgement, not a physical sensor.

Binary files have an eight-byte little-endian header (script ID, format marker 1), then six-byte records: `uint32 milliseconds`, `uint8 opcode`, `uint8 value`. Opcode 0 is target speed, 1 is stroke-minus valve, 2 is stroke-plus valve. Preserve ordering, duplicate timestamps and unknown opcodes. Position Funscript cannot recover valve events or original direction from a speed-only result. The offline tick model handles timing separately from the simple protocol client.

## Developer tools

- `cmd/atblse-simulator`: loopback-only WSS client with TLS verification, synthetic MAC and bounded downloads. `--lab --duration 10m` creates an isolated server. For a separately configured local server use `--url wss://127.0.0.1:20443/device-backend/ws --ca local-server.crt`. No insecure TLS switch, external endpoint or real credential is needed.
- `internal/simulator`: manual/valve/local/sync/slot protocol model. It reports requested state, does not schedule real-time playback, model a motor, emulate flash or accept OTA. `research/vacuglide_tick_model.py` is a separate offline timing model.
- `research/vacuglide_binary.py`: lossless binary decode/encode and candidate Funscript conversion. Example: `python research/vacuglide_binary.py decode input.bin output.json --csv analysis.csv`. Outputs must not exist already.
- `research/inspect_esp_image.py` and `research/ghidra/`: bounded static image inspection and Ghidra export scripts. Supply your own image; these do not flash a device. No vendor image is bundled.
- `research/virtual-cloud-probe.mjs`: archived opt-in cloud protocol experiment. Default is dry-run. Execution requires private input files, a new explicit operator decision and `VACUGLIDE_PROBE_POWER_OFF=CONFIRMED` after physically unplugging the owned real device. Never use this tool as the local simulator, never run it in CI, and never commit its inputs or output.
- `reference/vg_protocol.py`: original minimal Python codec for protocol comparison.

### Verification

```sh
go test -race ./...
go vet ./...
python -m unittest discover -s research -p 'test_vacuglide_binary.py' -v
python -m unittest discover -s research -p 'test_vacuglide_tick_model.py' -v
python scripts/test_package_release.py -v
node scripts/panel-contracts.mjs
python scripts/prepare-client-contracts.py
npm ci --prefix out/clients-integration --ignore-scripts --no-audit --no-fund
ATBLSE_TEST_NODE=$(command -v node) ATBLSE_TEST_CLIENTS="$PWD/out/clients-integration" go test -race -tags integration ./core
```

Node 24.18.0 supports the pinned TypeScript adapter harness; SDK version 2.6.0 is locked. The bundled adapter snapshot is an interoperability fixture; a live Studio install is a separate acceptance path. Dependency bootstrap needs internet or a prefilled cache, while tests and the built runtime can operate offline.

Use `node .standards/check.mjs staged` before committing. The owner uses an isolated build host for heavy operations; public PRs must never execute on a private runner automatically. GitHub's `repository-standard` job is a public, read-only tooling gate. The `software-verification` status identifies the exact source checked on the isolated build host; it is separate from physical acceptance.

Additional source checks: `node scripts/check-project.mjs` verifies versions and documentation links; `node scripts/test-standard-adoption.mjs` exercises the installed pinned checker in a disposable repository, including synthetic-secret rejection. Windows-only isolated tests: `powershell.exe -NoProfile -File scripts/test-state-migration.ps1` and `powershell.exe -NoProfile -File scripts/test-server-lifecycle.ps1`. These scripts use temporary state and mocks; they do not start the hotspot or touch a device.
