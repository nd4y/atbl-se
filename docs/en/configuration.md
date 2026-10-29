# Server configuration

[Русский](../ru/configuration.md) | [Installation](guide.md)

Pass JSON with `atblse-server --config server.json`. Unknown fields and trailing JSON are rejected. Values below are the standalone defaults when fields are omitted; the NAS example and Windows launcher explicitly override listener addresses and data paths. The simulator's isolated `--lab` mode enables compatible control and both script features, sets `maxSpeed` to 100 and uses temporary data with random loopback ports.

| JSON field                 | Default           | Meaning                                                             |
| -------------------------- | ----------------- | ------------------------------------------------------------------- |
| `deviceListen`             | `0.0.0.0:443`     | TLS listener for device WSS, binary downloads and compatible API    |
| `apiListen`                | `127.0.0.1:20880` | HTTP listener for panel, local API and compatible API               |
| `dnsListen`                | `""`              | Optional UDP/TCP DNS address; empty disables it                     |
| `dnsAnswer`                | `""`              | Private or loopback IPv4 returned by local DNS                      |
| `dataDir`                  | `data`            | Private persistent directory; relative to process working directory |
| `maxSpeed`                 | `15`              | Integer from 1 to 100; manual and sync-upload speed limit           |
| `allowShutdown`            | `false`           | Enable authenticated POST /api/shutdown                             |
| `compatibilityControl`     | `false`           | Allow external-client writes after local pairing                    |
| `experimentalSyncScripts`  | `false`           | Enable synchronized-script API                                      |
| `experimentalLocalScripts` | `false`           | Enable native slot/script API                                       |
| `scriptURLOrigins`         | `[]`              | Allowed HTTP(S) origins for script imports, at most 32              |
| `compatibilityCluster`     | `""`              | Optional bare host[:port] advertised to compatible clients          |
| `tlsCertificateFile`       | `""`              | PEM certificate path; set together with tlsPrivateKeyFile           |
| `tlsPrivateKeyFile`        | `""`              | PEM private key path; keep private                                  |

## Feature behavior

The panel can perform a bounded manual run after pairing without enabling the compatible write API. External writes require `compatibilityControl` and pairing. Script flags enable their respective routes. Native slot playback requires `maxSpeed: 100`; synchronized script uploads/load-token validate their encoded peak speed against `maxSpeed`. A compatible STOP remains available without enabling playback features. See the [API reference](api.md) for request bodies and STOP differences.

An empty `scriptURLOrigins` blocks all URL imports. Origins contain scheme, host and optional port, without credentials, query or path. Redirects are rejected. For a LAN source, use its explicitly allowlisted literal private IP; DNS hostnames resolving into private or loopback ranges are rejected. The fetcher forwards no device tokens, cookies or authorization headers.

Without certificate paths the server generates a self-signed TLS certificate at startup. Clients that verify TLS need a configured certificate and a trusted CA; do not disable verification. Both certificate paths must be provided together. Keep HTTP control access on a private network.

## Admission and storage limits

- One connected device; 30 native device slots.
- 32 ordinary HTTP handlers; two concurrent script uploads. Overflow returns HTTP 429. STOP/shutdown bypass ordinary admission but still require authentication.
- 32 event subscribers, 16 queued events per subscriber; a slow subscriber is disconnected.
- HTTP header reads: 5 seconds; request reads: 15 seconds. URL retrieval: 15 seconds total with bounded responses.
- Script input: 4 MiB and at most 100000 Funscript actions. Encoded time range: 0..16777214 ms. Each local store is limited to 128 records; abandoned pending files also consume quota.

These are implementation limits, not measured throughput guarantees. See [verification](verification.md).
