# Contributing

[Русский](CONTRIBUTING.ru.md)

Use a feature branch and a pull request. The owner performs merge. Read AGENTS.md and the pinned repository policy. Update both README files and relevant English/Russian documentation in the same PR. Keep changes scoped and preserve existing authorship/notices.

Bootstrap the checker with `npm ci --prefix .standards --ignore-scripts --no-audit --no-fund`, then `node .standards/install-gitleaks.mjs` and `node .standards/install-hooks.mjs`. The hook preserves prior hooks and checks staged content without restaging. Run `node .standards/check.mjs structure` and `node .standards/check.mjs staged`. Use the pinned Prettier settings in `.standards/format.json` and gofmt for Go.

Run the commands in docs/en/guide.md on isolated infrastructure. Do not connect to the official service, alter a network or use a real device as part of routine tests. Never execute an untrusted public PR on a private runner. Publish exact-source software evidence as the `software-verification` status after the reviewer's build; keep physical gates separate.
