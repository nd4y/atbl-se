# Verification and acceptance

[Русский](../ru/verification.md) | [Project scope](project.md)

The current candidate has software and virtual-protocol evidence. Physical acceptance and stable release are not complete. [Required PR statuses](https://github.com/nd4y/atblse/pull/2/checks) identify the exact submitted revision; recheck them after every change.

## Reproducible software evidence

The runtime at source `8a34d860cbd5b4ed0ac77146877fa930bdc12ea4` was verified with Go 1.27.3 and Node 24.22.1 on isolated Linux infrastructure, with network disabled for runtime tests. Build receipts cover 44 runtime source files. Documentation-only revisions preserve those hashes; the PR's `software-verification` status records the submitted source and current check results. Commands are in [development](development.md).

| Check                                        | Evidence and scope                                                                                          |
| -------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| Go race detector and vet                     | 524 tests/subtests; no race/vet findings in the verified run                                                |
| SDK 2.6.0 and Studio fixture                 | 57 requests over HTTP and 57 over HTTPS with virtual firmware                                               |
| Panel / binary codec / tick model / packager | 7 / 8 / 15 / 7 checks                                                                                       |
| Standalone simulator                         | 11 requests covering verified TLS, speed, both valves, STOP, slots and script transfer                      |
| Build                                        | Linux/Windows server and catalog importer; Windows GUI; source-bound receipts                               |
| Windows helpers                              | PowerShell 5.1 syntax and synthetic filesystem/process tests; no physical network activation                |
| Repository gates                             | Paired documentation, links, metadata, pinned formatting and staged/introduced/full-history secret scanning |

Gitleaks 8.30.1 includes merge patches in the full-history check. The verified toolkit also rejects bad formatting, missing/empty translations, a generated test secret and a missing scanner. The dependency audit used govulncheck v1.9.0 with the 2026-10-27 database: no reachable symbol findings; both locked npm development sets reported zero advisories. These dated observations are not a continuing vulnerability guarantee.

Protected `main` requires strict `repository-standard` and `software-verification` checks, including administrators. Secret scanning and push protection are enabled. Hosting settings and statuses are external state and must be read back for publication. The repository follows pinned standard 0.4.0 in adopting mode; no release/profile gate is waived by a software test.

## Standard applicability

| Rules         | Current evidence / remaining gate                                                                                                              |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| GOV-001..003  | Scope, journeys and pinned rules documented; capacity/recovery guarantees unmeasured                                                           |
| SEC-001..004  | Authentication, disconnect revocation, bounded parsing/origins, scanning and safe defaults; target permissions/operation still need acceptance |
| DATA-001..003 | Atomic state writes, bounded stores, backup and recovery instructions; target filesystem crash/restore checks open                             |
| REL-001..003  | Timeouts, cancellation, stale-generation protection and protocol STOP tested; real disconnect/stop/recovery open                               |
| CAP-001..003  | Request, upload, SSE, DNS and store bounds; representative resource/latency/scale measurements open                                            |
| UX-001..004   | Setup/lab journeys, stale state, focus and confirmation behavior; full accessibility and native installation acceptance open                   |
| OBS-001..003  | Health, version, status/errors and recovery instructions; installed operations open; alert-delivery subsystem not applicable                   |
| ARCH-001..003 | Versioned API/storage, pinned consumers and automated version agreement; live consumers and stable support open                                |
| AGT-001..004  | Scoped source work, inspected changes, exact-source evidence and separate physical/release gates                                               |
| REPO-001..006 | Experiment/adopting policy, scanning/formatting and meaningful EN/RU product documentation; exact PR head must pass                            |
| REPO-007..010 | Protected PR flow, isolated builds, verification boundaries and applicable profiles; installed acceptance open                                 |
| REPO-011..015 | Curated public inputs/notices, no hidden exceptions/private evidence; complete release/license/bootstrap gates still open                      |

Applicable profiles: web, cli, library, service, network, integration, data, firmware and docs. Mobile, infrastructure controller, monitoring and model-inference profiles are not applicable to the implemented product.

## Remaining acceptance

- Real Go-candidate playback, both valves, slot changes, stop/disconnect and power-loss behavior on the supported device.
- Native Windows Hotspot provisioning/DHCP/DNS and router/NAS paths, including cold startup without WAN.
- Clean installation, target permissions, backup/restore and failure recovery on supported physical platforms.
- Complete accessibility and representative resource/latency measurements.
- Stable release artifacts, product-wide license decision and installed artifact provenance.

No real device, router, NAS deployment or firmware operation is part of the offline verification suite. Protocol acknowledgement and virtual playback cannot establish physical motion or stopping.
