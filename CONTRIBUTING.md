# Contributing

Thanks for wanting to help. This is a small, precise tool — keep it that way.

## Ground rules

Read [AGENTS.md](AGENTS.md) before anything else. The hard constraints (offline-first,
CPU-only, no heavyweight deps, never commit `work/` or `output/`) are not suggestions.

## Development

```sh
go build ./...          # must be clean
go vet ./...            # must be clean
gofmt -l .              # must print nothing
go test ./...           # hermetic: no network, no real binaries

scripts/integration.sh  # offline end-to-end test (needs ffmpeg; whisper optional)
scripts/clean.sh        # wipe work/ + output/ (safe by default, -y to skip prompt)
```

## Structure

```
cmd/ytreconstruct/    CLI entrypoint (cobra)
internal/             one package per pipeline stage, JSON contracts on disk
docs/                 architecture, instructions, build brief, progress log
scripts/              clean.sh, integration.sh, ocr.ps1
```

Package responsibilities and the on-disk contracts live in
[docs/architecture.md](docs/architecture.md). Read it before touching a package.

## Making changes

- Small, reviewable commits over one giant commit.
- Every package gets a test file. Mock `os/exec` — tests must never require
  network access or the real binaries.
- Before opening a PR: `go build ./...`, `go vet ./...`, `gofmt -l .` all clean,
  `go test ./...` green.
- Say what you tested and what you didn't. Never fabricate a result.
