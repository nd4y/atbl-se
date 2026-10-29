# Contributing

[Русский](CONTRIBUTING.ru.md)

Use a feature branch and a pull request. The owner merges after required checks. Read [repository instructions](AGENTS.md), [.repo-policy.json](.repo-policy.json) and the pinned [development standard](docs/en/development-standard.md). Keep both READMEs and relevant English/Russian pages current in the same change. Preserve existing authorship and third-party notices.

Follow [development commands](docs/en/development.md) for dependencies, checks and tools. Install the pinned hooks before committing; they check staged content without restaging and preserve previous hooks. Use Go's formatter and the pinned Prettier configuration. Missing secret scanning blocks publication.

Run builds and full tests on isolated infrastructure. Routine tests use synthetic inputs and a loopback device; they do not access the official service, change networking or operate hardware. Never execute untrusted public PR code automatically on a private runner. Keep credentials, private state, captures, firmware images and vendor media outside Git.

Tie the `software-verification` status to the exact reviewed source and evidence. Report test scope and limitations; separate source checks from installation, physical acceptance and release. [Verification](docs/en/verification.md) lists the open gates. Do not claim product-wide licensing rights that have not been granted.
