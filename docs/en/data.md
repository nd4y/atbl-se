# Data, catalog files and recovery

[Русский](../ru/data.md) | [Configuration](configuration.md)

## Private state

| Location under `dataDir` | Contents                                                                    |
| ------------------------ | --------------------------------------------------------------------------- |
| `control-token`          | Local control key; possession grants control access                         |
| `paired-device`          | Explicitly paired device MAC                                                |
| `sync-scripts/`          | Converted sync-script records with integrity hashes                         |
| `local-catalog/`         | Imported native binary records keyed by catalog UUID, with integrity hashes |

Windows stores this server state under `%LOCALAPPDATA%/atblse/server`; launcher configuration and hotspot credentials live under `%LOCALAPPDATA%/atblse`. Keep the whole directory private. Standalone configuration and custom TLS files may live outside `dataDir`; include them in your backup inventory.

State writes use a temporary file, flush and atomic publication. Failed/pending files count toward store limits; do not delete them while the service is running. Crash/power-loss durability on the target filesystem still requires acceptance. The server stores no vendor account or video library. Installed native slot content resides on the device and is not a server-state backup.

## Import a native catalog file

Use a binary file you may lawfully use and its matching catalog UUID. Build the importer from the same source as the server:

```sh
go build -o out/atblse-catalog-import ./cmd/atblse-catalog-import
./out/atblse-catalog-import --data-dir data --uuid <catalog-uuid> --file <script.bin>
```

Angle-bracket values are placeholders. On Windows use the `.exe` filename. The importer validates the native format and stores the file locally; it sends no device command. It does not download a catalog or invent a UUID. To replace a device slot, an authorized external client must subsequently call `/vacuglide/local-script/replace` with `replacedLocalScriptIndex` and `newLocalScriptUuid`, with pairing and the required flags enabled. Original catalog identity metadata is bundled; the manufacturer's binary library is not.

For offline inspection, choose output paths that do not already exist:

```sh
python research/vacuglide_binary.py decode input.bin output.json --csv analysis.csv
```

The decoder preserves native events, including valves. Position Funscript is not a lossless replacement for native binary files. See [API format](api.md) and the tick-model tools in [development](development.md). File availability does not grant redistribution rights; do not commit private captures or vendor content.

## Backup and restore

1. Stop playback and confirm the server process has exited. In Windows use the launcher's stop action before closing it.
2. Copy the private data directory, configuration and custom TLS key/certificate to protected storage. Keep permissions restrictive and record the server version.
3. Restore to a separate directory first. Start with loopback-only listeners and no real device/network redirection. Verify configuration parsing, health, key authentication and stored-file integrity before using the copy.
4. Stop that test instance. Select the restored directories in the intended configuration and verify permissions for the actual service account.

Never run two servers against the same state directory. Preserve the original backup until the restored instance is checked. A restore copies the control key: anyone holding that backup also holds that key. Physical filesystem restore and device reconnection remain separate acceptance tasks.

## Revoke or recover the local key

Stop the server. Restore a known-good `control-token` from a private backup, or remove only that file to revoke the key and let the next start generate a new one. Update saved client credentials. Do not erase pairing or script directories to solve a key problem. Invalid key files stop startup rather than silently generating a replacement. After rotation, verify the old key is rejected and the new key works; do not attach either value to an issue.
