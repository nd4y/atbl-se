# Installation and local network

[Русский](../ru/guide.md) | [README](../../README.md)

Use the [Windows launcher](windows.md) for a desktop installation. The steps below cover the standalone server and NAS. Prepare binaries, images and configuration before disconnecting the internet. Device operation uses local networking; downloading a script from an external URL still requires that source to be reachable.

## Standalone server

Run these commands from the repository root:

```sh
go build -o out/atblse-server ./cmd/atblse-server
./out/atblse-server --version
```

Create a private `server.json`. This example listens only on loopback and is suitable for local client development:

```json
{
  "deviceListen": "127.0.0.1:20443",
  "apiListen": "127.0.0.1:20880",
  "dataDir": "data",
  "maxSpeed": 15
}
```

```sh
./out/atblse-server --config server.json
```

Open `http://127.0.0.1:20880/` and enter the key from `data/control-token`. `GET /health` reports version `0.2.0-rc.3`; it does not prove a device is connected. `--healthcheck --config server.json` checks that endpoint and returns a nonzero exit code on failure. Stop the server with Ctrl+C or SIGTERM; shutdown attempts STOP before closing the device connection.

For physical hardware, bind `deviceListen` to a suitable LAN address and deliver the device's TCP 443 traffic to that listener. Configure DNS as described below. Keep the control API on loopback unless a remote client needs it. All [configuration fields](configuration.md) are explicit; relative data paths are resolved from the process working directory.

## NAS Docker

The supplied Dockerfile expects a prebuilt Linux binary and a CA bundle. On a Linux build machine, from the repository root:

```sh
mkdir -p out/image
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o out/image/atblse-server ./cmd/atblse-server
cp /etc/ssl/certs/ca-certificates.crt out/image/ca-certificates.crt
docker build -f deploy/Dockerfile -t atblse:0.2.0-rc.3 out/image
docker save -o out/atblse-image.tar atblse:0.2.0-rc.3
```

The example targets amd64. Select the correct architecture for the NAS; other architectures need their own acceptance. Transfer the image archive, `deploy/compose.yaml` and `deploy/server.example.json` to the NAS. Load the image with `docker load -i atblse-image.tar`.

Prepare two private absolute directories: data and configuration. Put `server.example.json` into the configuration directory as `server.json`. Set `ATBLSE_DATA_DIR` and `ATBLSE_CONFIG_DIR` to those paths. Container UID/GID 65532 needs write access to data and read access to configuration. Start explicitly with:

```sh
docker compose -f deploy/compose.yaml up -d emulator
```

Compose uses `pull_policy: never`, `restart: no`, a read-only root filesystem, no added capabilities, 96 MiB RAM and 0.5 CPU. Host TLS port 20445 maps to container 20443; host API port 20881 maps to 20880. DNS is disabled in the example and no DNS port is published by Compose. Publishing a repository or loading an image does not start the service.

## Router and DNS choices

The device connects to `latency.autoblowapi.com` and regional hosts under `autoblowapi.com`. Route only the intended device to the local service. A DNS record changes an address, not a port.

1. **Device-specific DHCP DNS.** On OpenWrt/dnsmasq, assign the device a DHCP tag and option 6 pointing to a dedicated local resolver. Resolve the relevant `autoblowapi.com` names to the local device listener. It must be reachable as TCP 443 from the device.
2. **Resolver overrides.** Use router, Pi-hole or AdGuard DNS overrides for `latency.autoblowapi.com` and the selected regional host, or a supported wildcard zone. Shared resolver changes affect other clients too. Do not leave a public AAAA answer competing with the intended local route.
3. **NAS with port 443 already occupied.** Resolve the device's service names to the router LAN address, then DNAT only that device's TCP 443 to the NAS TLS port 20445. Use matching hairpin SNAT when necessary. If intercepting DNS, direct only that device's UDP/TCP 53 to a local resolver. Traffic sent directly between hosts on the same subnet does not pass through router DNAT.

These are configuration patterns for OpenWrt/dnsmasq and MikroTik, not a router automation script. Keep an independent route to router administration. Verify actual matches and counters, then confirm connection and pairing in the panel. Restore only the DHCP/DNS/NAT entries you changed if the path does not work. Do not expose the control or device ports to the internet. Real-device network, restart and WAN-loss acceptance is still pending.
