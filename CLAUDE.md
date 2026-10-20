# CLAUDE.md

## Build & test
- All Go commands need `GOFLAGS=-mod=vendor` (deps are vendored, including the `jesseduffield/gocui` fork and the Docker SDK).
- Unit tests: `GOFLAGS=-mod=vendor go test ./...`
