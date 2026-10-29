#!/bin/sh
# Run inside the pinned Go build image on build-01, with --network none.
set -eu
: "${AUTOBLOW_RESULTS:?set writable results directory}"
go test -race -json ./... > "$AUTOBLOW_RESULTS/race.jsonl" 2>&1
go vet ./... > "$AUTOBLOW_RESULTS/vet.log" 2>&1
go test -race -tags integration -run 'TestSDKAndStudioManualContracts|TestDiagnosticPanelIntegration' -v ./core > "$AUTOBLOW_RESULTS/integration.log" 2>&1
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -o "$AUTOBLOW_RESULTS/autoblow-server" ./cmd/autoblow-server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -o "$AUTOBLOW_RESULTS/autoblow-catalog-import" ./cmd/autoblow-catalog-import
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -o "$AUTOBLOW_RESULTS/autoblow-server.exe" ./cmd/autoblow-server
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -o "$AUTOBLOW_RESULTS/autoblow-catalog-import.exe" ./cmd/autoblow-catalog-import
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags='-H windowsgui' -o "$AUTOBLOW_RESULTS/AutoblowLocal.exe" ./cmd/autoblow-windows
cp /etc/ssl/certs/ca-certificates.crt "$AUTOBLOW_RESULTS/ca-certificates.crt"
cp /usr/share/doc/ca-certificates/copyright "$AUTOBLOW_RESULTS/ca-certificates-copyright"
"$AUTOBLOW_RESULTS/autoblow-server" --version > "$AUTOBLOW_RESULTS/version.txt"
go version > "$AUTOBLOW_RESULTS/go-version.txt"
"$AUTOBLOW_TEST_NODE" scripts/build-receipt.mjs
