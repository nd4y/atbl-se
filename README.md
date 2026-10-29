# atblse

[Русский](README.ru.md)

![Local server and virtual VacuGlide device](docs/assets/preview.svg)

**atblse - Autoblow Server Emulator** runs a stock VacuGlide 2 through a local server. It provides a diagnostic web panel and a compatible control API for applications such as VacuGlide Studio.

- Operates without internet access or a cloud account once the application and configuration are available locally.
- Controls speed, both valves, 30 device slots and synchronized Funscript/CSV playback.
- Runs as a Go server, through a Windows launcher, or in a NAS Docker container with automatic restart disabled.
- Includes a virtual device for development without hardware.

The current source version is **0.2.0-rc.3**, an experimental candidate for VacuGlide 2 firmware **1.03 / 1.0.3**. Physical acceptance of this Go implementation and Windows Hotspot remains open. AI Ultra and firmware updates are outside the supported profile.

## Try it without a device

From the repository root, with Go 1.24 or later:

```sh
go build -o out/atblse-simulator ./cmd/atblse-simulator
./out/atblse-simulator --lab --duration 10m
```

On Windows, use `-o out/atblse-simulator.exe` and run that file. The program prints JSON with `api`, `controlToken` and `pairMAC`. Open `api` in a browser, enter the key and pair the displayed virtual device. The lab uses temporary data and exits after ten minutes. Its TLS certificate is verified; no official service or physical device is involved.

A source build needs downloaded dependencies or a prepared cache. The built application does not download dependencies at startup. The web panel and Windows launcher currently use Russian labels.

## Connect a device or application

Choose [Windows](docs/en/windows.md) or the [standalone server / NAS guide](docs/en/guide.md). Configure the local network, connect the device and confirm pairing in the panel. For an external application, set the local server address and control key, then explicitly enable the required API features.

The standalone server and Windows launcher default to a maximum speed of 15%, with script features disabled. The synthetic lab enables script features and uses a 100% limit for virtual tests. The panel uses a renewable control session; external applications manage their own STOP behavior. Firmware acknowledgement of STOP is not a physical sensor reading.

## Documentation

- [Installation and router/DNS options](docs/en/guide.md)
- [Windows launcher](docs/en/windows.md)
- [Configuration reference](docs/en/configuration.md)
- [API, scripts and valves](docs/en/api.md)
- [Data, backup and key recovery](docs/en/data.md)
- [Development and verification commands](docs/en/development.md)
- [Architecture and scope](docs/en/project.md), [verification status](docs/en/verification.md)
- [Contributing](CONTRIBUTING.md), [security](SECURITY.md), [development standard](docs/en/development-standard.md)
- [Releases](https://github.com/nd4y/atblse/releases), [issues](https://github.com/nd4y/atblse/issues)

No stable release or project-wide open-source license is declared. See [LICENSE.md](LICENSE.md) and [third-party notices](THIRD_PARTY_NOTICES.md). The repository contains no device credentials, session captures, firmware images or video-library collection.
