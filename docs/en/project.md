# Project passport and acceptance

[Русский](../ru/project.md)

**Product:** standalone offline device-control service and diagnostic panel, candidate 0.2.0-rc.2; experimental lifecycle. Audience: owners of stock VacuGlide 2 and developers of compatible clients.

| Journey                  | Expected result                                            | Acceptance                                                                       |
| ------------------------ | ---------------------------------------------------------- | -------------------------------------------------------------------------------- |
| Connect locally          | Identified device, explicit pairing, independent local key | TLS/WSS and negative-auth tests; physical network path still pending             |
| Control through a client | Confirmed manual/valve/script/slot state and STOP          | Official SDK 2.6.0 and bundled Studio HTTP adapter contracts on virtual firmware |
| Develop without hardware | Loopback lab, synthetic replies, local file transfers      | Standalone simulator integration and malformed-input tests                       |

The portable Go server owns WSS, HTTP/SSE, DNS, local authentication and scenario storage. Windows owns Wi-Fi Direct and firewall lifecycle. Docker hosts the core independently. Linux/macOS native wrappers and Android hosting are design possibilities, not implemented applications. No dependency on a cloud account, registry at startup or proprietary browser session.

Persistent data includes local control key, pairing identity, configuration and imported scripts. Windows also stores its own hotspot password. Stop the process before backing up the entire private data/config directories; validate an isolated restore before relying on it. Raw data is not suitable for bug-report attachments. Release assets must be built and tied to exact sources; no stable public release or automatic deployment is claimed by the initial source PR.

The reference profile implements 26 control operations, original catalog identity and binary transfer. Accounts, video catalog, community, AI and OTA are excluded. Operator-visible policy differences include local pairing, explicit control/script flags, origin allowlisting, bounded storage and independently managed data. See the machine-readable contract.

Existing software evidence from candidate review: 515 Go tests/subtests under race detection, go vet, 57 SDK/adapter requests each over HTTP and HTTPS, six panel regressions, eight codec tests, four packager tests and five mocked Windows lifecycle checks. The publication adds simulator cases and reruns checks on its exact head; the PR and `software-verification` status are authoritative for that revision.

**Not verified:** physical Go-candidate playback/valve behavior, native Windows Hotspot end to end, cold startup and power/network loss on the target device, complete accessibility, clean install/upgrade/restore on supported physical targets, and advertised scale beyond tested bounds. The generic repository-standard job cannot establish these outcomes.

Limits include one connected device, 30 native slots, 128 imported catalog records, 100000 source Funscript actions and bounded script transfer. Docker's candidate resource budget is 96 MiB/0.5 CPU; it is a test configuration, not a throughput guarantee. Panel runs are 1..3600 seconds with a two-second keepalive lease. There is no guarantee of physical stop after controller or link failure. No stable SLA, recovery time or data-loss guarantee is advertised before target acceptance.

Support covers the documented candidate and firmware profile. File reproducible issues with version, sanitized configuration, expected/actual result and synthetic inputs. Never attach tokens, full state directories, session captures, vendor images or video-library files. Prior prototype acceptance is historical and does not qualify the current build.
