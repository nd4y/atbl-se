# Repository instructions

Read .repo-policy.json, the English or Russian guide and relevant pinned standard rules. Use branches and PRs; the owner performs merge. Keep README.md, README.ru.md and relevant docs/en and docs/ru pages meaningful and current in the same change. Do not weaken required checks or change the documentation policy.

Core targets stock VacuGlide 2 firmware1.03/1.0.3 only. Device/cloud/router/NAS operations and motor activation are outside routine code work. Keep physical acceptance separate from protocol simulation. Never resume video-library capture or send real credentials to a worker or CI.

Run go test -race ./..., go vet ./..., the Python codec/tick/packager tests, Node panel contracts and the SDK integration commands in the guide. New simulator work also requires scripts/test-simulator.py against the compiled CLI. Use gofmt, pinned Prettier and staged/full-history Gitleaks; unavailable scanning blocks publication. Build on isolated infrastructure and never admit untrusted PR code to a private runner automatically. The owner's build-host policy applies to agent operations.

Runtime state, credentials, captures, firmware images, vendor video files and build output are excluded. Keep only explicitly reviewed tools and synthetic/limited interoperability fixtures. Preserve third-party notices and do not claim a license or physical compatibility not established by evidence. Stop when the authorized source/publication stage is verified; deployment and owner merge are distinct actions.
