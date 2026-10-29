# Migration and recovery

[Русский](../ru/migration.md) | [Installation](guide.md)

## Rename from 0.2.0-rc.2

Stop the old server and launcher before replacing binaries. Keep the complete old package and a private copy of data/configuration. Never run old and new packages simultaneously.

| Previous name                                | Current name                             |
| -------------------------------------------- | ---------------------------------------- |
| `ATBL-SE`, repository `nd4y/atbl-se`         | `atblse`, repository `nd4y/atblse`       |
| `autoblow.local/emulator`                    | `github.com/nd4y/atblse`                 |
| `AutoblowLocal.exe`                          | `atblse.exe`                             |
| `autoblow-server`, `autoblow-catalog-import` | `atblse-server`, `atblse-catalog-import` |
| `vacuglide-simulator`                        | `atblse-simulator`                       |
| `cmd/autoblow-windows`                       | `cmd/atblse-windows`                     |
| `AUTOBLOW_*` environment variables           | `ATBLSE_*`                               |
| `%LOCALAPPDATA%/AutoblowLocal`               | `%LOCALAPPDATA%/atblse`                  |

The other command directories match the new binary names. Update shortcuts, build scripts and Compose variables. Existing NAS data and config mounts stay at their explicitly selected locations: a project rename must not create empty replacement volumes. The JSON configuration and stored script record version remain compatible. Official `/autoblow/*`, `/vacuglide/*`, device headers/domains and SDK names are protocol contracts, not old branding.

## Windows data

On first launch the wrapper holds both the old and new launcher locks. It refuses migration while the old API port is occupied. It copies the legacy tree to a unique sibling staging directory, flushes files, verifies SHA-256 and publishes the complete directory by rename. The original tree remains a backup. Limits: 4096 entries and 1 GiB; links/junctions are rejected. The migration marker records version 1 and file hashes without file contents. Existing current data is reused; two unmarked data directories cause an error instead of a silent merge. Failed staging copies remain inert, with a new attempt using another staging directory.

The saved Wi-Fi name and password are preserved, including an older SSID. Only fresh installations default to `atblse`. Firewall setup validates the executable/interface before replacing the three application-owned rules and their legacy names; it requires the usual explicit elevation. Do not remove unrelated rules.

For rollback, stop the new application and restore the complete pre-migration backup with the old package. Never merge active state trees or assume old data includes later changes made by the new version. Native Hotspot and installed-package upgrade remain physical acceptance gates; isolated migration tests do not prove them.

## State, key revocation and backup

Only flushed complete state files become visible. Linux commits also flush the parent directory; Windows uses a write-through rename. Startup rejects malformed local keys and pairing files with actionable errors. A failed commit can leave `.pending-*`; these files are never loaded and count toward storage quotas. With every writer stopped, move them into a private recovery backup before retrying. A stale catalog `.import-lock` may be removed only after confirming no importer is running.

Back up the full data and config directories while stopped. To test restoration, copy them to a private temporary location, configure loopback listeners and use the synthetic client; do not route the real device to that server. Verify key identity, pairing and script checksums before replacing live data. Delete exports only by an explicit owner decision.

To revoke the local key: stop the server, back up state, remove only `control-token`, restart, and update each client with the newly generated key. This also closes old streams. To repair damaged pairing, restore `paired-device` from the backup or, while stopped, remove only that file and pair again. Leave script files intact. Device credentials and their event streams are valid only for the current device connection; the independent local key can still inspect offline status.

Two script uploads may run concurrently; further uploads receive HTTP 429. Body reads are bounded to 15 seconds. Cancellation releases admission; STOP does not consume an upload permit. Event streams are limited to 32 subscribers and 16 queued events each. These are admission limits, not a measured throughput or physical-safety guarantee.

Ordinary API and binary-download handlers share 32 concurrent slots. Overflow receives HTTP 429 with `Retry-After: 1`; STOP and shutdown bypass this quota but still require authorization. SSE and the device WebSocket have separate limits. This does not guarantee connectivity during socket-level denial of service.
