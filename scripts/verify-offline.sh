#!/bin/sh
# Run inside the pinned Go build image with --network none.
set -eu
: "${ATBLSE_RESULTS:?set writable results directory}"
go test -race -json ./... > "$ATBLSE_RESULTS/race.jsonl" 2>&1
go vet ./... > "$ATBLSE_RESULTS/vet.log" 2>&1
go test -race -tags integration -run 'TestSDKAndStudioManualContracts|TestDiagnosticPanelIntegration' -v ./core > "$ATBLSE_RESULTS/integration.log" 2>&1
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -o "$ATBLSE_RESULTS/atblse-server" ./cmd/atblse-server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -o "$ATBLSE_RESULTS/atblse-catalog-import" ./cmd/atblse-catalog-import
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -o "$ATBLSE_RESULTS/atblse-server.exe" ./cmd/atblse-server
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -o "$ATBLSE_RESULTS/atblse-catalog-import.exe" ./cmd/atblse-catalog-import
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags='-H windowsgui' -o "$ATBLSE_RESULTS/atblse.exe" ./cmd/atblse-windows
cp /etc/ssl/certs/ca-certificates.crt "$ATBLSE_RESULTS/ca-certificates.crt"
cp /usr/share/doc/ca-certificates/copyright "$ATBLSE_RESULTS/ca-certificates-copyright"
"$ATBLSE_RESULTS/atblse-server" --version > "$ATBLSE_RESULTS/version.txt"
go version > "$ATBLSE_RESULTS/go-version.txt"
"$ATBLSE_TEST_NODE" scripts/build-receipt.mjs
