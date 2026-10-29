# Development and verification commands

[Русский](../ru/development.md) | [Contributing](../../CONTRIBUTING.md)

## Environment and source layout

The module requires Go 1.24 or later. The verified build toolchain is Go 1.27.3. Client tests use Node 24 (CI pins 24.18.0; the isolated software run uses 24.22.1), Python 3 and the locked `@xsense/autoblow-sdk` 2.6.0 dependency. Bootstrap needs network access or prepared caches. The built core and offline tests do not require the official service.

| Path                                          | Responsibility                                                     |
| --------------------------------------------- | ------------------------------------------------------------------ |
| `cmd/atblse-server`, `core/`                  | Server CLI, TLS/WSS, HTTP/SSE, DNS, persistence and embedded panel |
| `internal/protocol/`                          | Commands, reply validation and script conversion                   |
| `cmd/atblse-simulator`, `internal/simulator/` | Synthetic loopback device and temporary lab                        |
| `cmd/atblse-catalog-import`                   | Offline native catalog import                                      |
| `cmd/atblse-windows`, `windows/`              | Windows launcher and network/process lifecycle                     |
| `deploy/`                                     | Explicit-start Docker configuration                                |
| `tests/clients/`                              | Locked SDK dependencies and Studio adapter fixture                 |
| `research/`, `reference/`                     | Offline format/timing/static-analysis tools and protocol reference |
| `.standards/`, `scripts/`                     | Pinned repository checks, tests and packaging                      |

Build commands are in [installation](guide.md) and [Windows](windows.md). For an isolated lab use the [README quickstart](../../README.md). The simulator accepts `--duration` from `1s` through `1h`. `--lab` creates ephemeral state and verified loopback TLS; it cannot be combined with a custom URL/CA. To attach to a separately configured loopback server:

```sh
./out/atblse-simulator --url wss://127.0.0.1:20443/device-backend/ws --ca local-server.crt --duration 60s
```

The certificate must cover the URL host and belong to that server. Non-loopback destinations are rejected. The simulator does not model physical mechanics; use `research/vacuglide_tick_model.py` for the offline timing model.

## Required software checks

Run heavy checks on isolated build infrastructure. On a POSIX shell with the dependencies available:

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
go build -o out/atblse-simulator ./cmd/atblse-simulator
python scripts/test-simulator.py --binary out/atblse-simulator
node scripts/check-project.mjs
```

Prepare dependencies before disabling network. The integration suite exercises SDK and Studio fixtures over HTTP and HTTPS with a synthetic device. The standalone simulator test covers its CLI, certificate trust, bounded session and command/file exchanges. Windows-only helper tests need Windows PowerShell 5.1; mocked process/network tests do not establish real hotspot acceptance.

For a pinned Linux build container, `scripts/verify-offline.sh` runs Go race/vet/client checks and builds Linux/Windows server, importer and Windows GUI. Supply writable `ATBLSE_RESULTS`, executable `ATBLSE_TEST_NODE` and prepared `ATBLSE_TEST_CLIENTS`; mount sources read-only, provide the Go module/build caches and disable container networking. Its build receipt binds binaries to runtime source hashes. The separate Python, panel and simulator checks above are still required.

## Repository gates

```sh
npm ci --prefix .standards --ignore-scripts --no-audit --no-fund
node .standards/install-gitleaks.mjs
node .standards/install-hooks.mjs
node .standards/check.mjs structure
node .standards/check.mjs staged
node scripts/test-standard-adoption.mjs
```

The hook checks staged bytes and preserves previous hooks. Use Go's formatter and the pinned Prettier configuration; do not modify the bundled standard to make a failing check pass. Run the pinned Gitleaks scanner on staged changes, introduced commits and full history, including merge patches. Missing scanning blocks publication. Never execute untrusted public PR code on a private runner automatically.

GitHub `repository-standard` is the public read-only tooling gate. `software-verification` records checks of an exact source revision on isolated infrastructure. Neither status establishes installation, physical behavior or a stable release. The owner merges PRs after the required checks.

## Research and packaging boundaries

`research/vacuglide_binary.py` decodes native records; `research/vacuglide_tick_model.py` models timing. `research/inspect-esp32-image.py` and `research/ghidra/` inspect operator-supplied images without flashing. `reference/vg_protocol.py` is a compact codec reference. No firmware image or private capture is bundled.

`research/virtual-cloud-probe.mjs` is an optional cloud experiment, not part of offline tests. It defaults to dry-run. Executing it requires private inputs, explicit operator authorization and `VACUGLIDE_PROBE_POWER_OFF=CONFIRMED` only after physically unplugging the owned device. Never use real tokens or run this tool in CI.

`scripts/package-release.py` takes `--build-dir`, `--output-dir` and `--source-manifest`. It packages prebuilt artifacts only after validating source hashes, binary receipts, image metadata and required notices. The output directory must not exist. A package is not a published or physically accepted release. See [verification](verification.md) for remaining gates and [license status](../../LICENSE.md) before redistribution.
