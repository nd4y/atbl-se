# Windows launcher

[Русский](../ru/windows.md) | [Configuration](configuration.md)

The launcher uses Windows PowerShell 5.1, .NET Framework and WinRT. Runtime operation does not require Python, WSL or Docker Desktop. Autonomous Wi-Fi requires an adapter supporting Wi-Fi Direct.

## Prepare and run

From the repository root in Windows PowerShell, with Go installed:

```powershell
New-Item -ItemType Directory -Force out/windows | Out-Null
go build -o out/windows/atblse-server.exe ./cmd/atblse-server
go build -ldflags="-H windowsgui" -o out/windows/atblse.exe ./cmd/atblse-windows
Copy-Item -Recurse -Force windows out/windows/
Copy-Item -Recurse -Force docs out/windows/
```

Keep `atblse.exe`, `atblse-server.exe`, `windows/` and `docs/` together. Open `atblse.exe`; the server is initially stopped.

1. Select the home-network mode and its IPv4 interface, or the autonomous access point.
2. In access-point mode, choose an SSID and password. In home-network mode, configure the router/DNS path from the [network guide](guide.md).
3. Click **Запустить**. The launcher starts its server and opens the authenticated local panel.
4. Use **Разрешить в брандмауэре...** if needed. This requests elevation and limits the application rules to DNS/TLS, the selected interface and the local subnet.
5. Configure the device through its normal Wi-Fi setup, connect it and confirm pairing in the panel. This step requires the physical device.

The control panel listens on `127.0.0.1:20880`. The device listener uses port 443 on the chosen interface. Autonomous mode enables the local DNS service; home-network mode has a DNS checkbox. Starting the launcher alone does not start the server or motor.

## Data and control options

Application data is in `%LOCALAPPDATA%/atblse`; server state is in its `server/` subdirectory. Wi-Fi credentials are saved locally. Optional `control-options.json` accepts only `maxSpeed`, `compatibilityControl`, `experimentalSyncScripts`, `experimentalLocalScripts` and `scriptURLOrigins`. Edit it while the server is stopped. See [configuration](configuration.md) and [data recovery](data.md).

## Stop and diagnose

Use **Остановить** or close the window. The launcher checks ownership of its API listener and requests server shutdown before removing its access point. If it cannot confirm process exit, it keeps the network available and offers another stop attempt.

Read `server-error.log`, `hotspot-error.log` and `launcher-startup.log` in the application data directory. Startup errors from the executable are written to `%LOCALAPPDATA%/atblse-launcher-error.log`. Do not publish raw logs containing personal paths or configuration. A successful UI startup does not establish Hotspot/device acceptance; see [verification status](verification.md).
