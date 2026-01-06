# CLAUDE.md - lazypodman

## Project Overview

This is a fork of **lazydocker** being converted to **lazypodman** - a terminal UI for managing Podman containers.

**Goals:**
1. Support native libpod (Podman's Go library) instead of Docker SDK
2. Enable socket-less operation for Podman commands (no daemon required)

**Current State:** The codebase uses Docker Go SDK (`github.com/docker/docker` v28.5.2) and requires conversion to libpod.

## Quick Start

```bash
# Build
go build -mod=vendor

# Run
./lazypodman

# Run tests
./test.sh

# Run with debug logging
./lazypodman -d

# Run with specific compose files
./lazypodman -f docker-compose.yml -f docker-compose.override.yml
```

## Architecture

### Package Structure

```
pkg/
├── app/           # Application initialization and lifecycle
├── commands/      # Container runtime interaction (Docker -> Podman target)
├── gui/           # Terminal UI (gocui-based)
│   ├── panels/    # Reusable panel components
│   └── presentation/  # Display formatting
├── config/        # Configuration management
├── i18n/          # Internationalization (9 languages)
├── tasks/         # Background task queue
├── utils/         # Helper utilities
├── log/           # Logging setup
└── cheatsheet/    # Keybinding reference
```

### Key Files for Podman Integration

**Primary targets for libpod conversion:**
- `pkg/commands/docker.go` - Main client connection and initialization
- `pkg/commands/container.go` - Container operations
- `pkg/commands/image.go` - Image operations
- `pkg/commands/volume.go` - Volume operations
- `pkg/commands/network.go` - Network operations
- `pkg/commands/service.go` - Compose service operations
- `pkg/commands/docker_host_unix.go` - Unix socket detection
- `pkg/commands/docker_host_windows.go` - Windows pipe detection

**Application entry:**
- `main.go` - Entry point, CLI flags
- `pkg/app/app.go` - App struct, initialization flow

## Current Docker Integration

### Connection Flow
1. `NewDockerCommand()` in `docker.go` initializes client
2. Docker host determined from `DOCKER_HOST` env or platform defaults
3. Uses `github.com/docker/docker/client` SDK for API calls
4. SSH tunneling supported for remote hosts

### API Methods Used
```go
// Container operations
Client.ContainerList()
Client.ContainerInspect()
Client.ContainerStats()      // Streaming
Client.ContainerStart/Stop/Pause/Unpause/Restart/Remove()
Client.ContainersPrune()

// Image operations
Client.ImageList()
Client.ImagesPrune()

// Volume/Network
Client.VolumeList/VolumesPrune()
Client.NetworkList/NetworksPrune()
```

### Command Execution Patterns
1. **SDK calls** - For most container/image operations
2. **Shell commands** - For interactive operations (attach, logs) and compose
3. **Template-based** - Configurable command templates in config

## Libpod Integration Path

### Required Changes

1. **Replace Docker SDK with libpod bindings**
   - Add `github.com/containers/podman/v5/pkg/bindings` dependency
   - Replace `*client.Client` with libpod connection

2. **Update type mappings**
   - Docker `container.Summary` -> Podman equivalent
   - Docker `image.Summary` -> Podman equivalent
   - All API response types need mapping

3. **Socket-less mode**
   - Implement direct libpod calls without socket
   - Use `bindings.NewConnection()` with appropriate URI

4. **Compose support**
   - Detect `podman-compose` vs `docker-compose`
   - Update command templates

### Files NOT requiring changes
- `pkg/gui/*` - UI layer is container-runtime agnostic
- `pkg/config/*` - Configuration structure remains same
- `pkg/i18n/*` - Translations unaffected

## Development Guidelines

### Build System
- Uses vendored dependencies (`go build -mod=vendor`)
- GoReleaser for releases (`.goreleaser.yml`)
- No Makefile - use `go build` directly

### Code Patterns
- Generic panels: `SideListPanel[T]` for type-safe list handling
- Mutex protection: `deadlock.Mutex` for concurrent access
- Error handling: Custom errors in `pkg/commands/errors.go`
- Platform-specific: `*_windows.go`, `*_unix.go`, `*_default_platform.go`

### Configuration
- Config path: `~/.config/lazypodman/config.yml` (Linux)
- Custom commands configurable via YAML
- Command templates use Go template syntax

## Testing

```bash
# Run all tests with coverage
./test.sh

# Run specific package tests
go test -mod=vendor ./pkg/commands/...
go test -mod=vendor ./pkg/gui/...
```

## Module Info
- Module: `github.com/christophe-duc/lazypodman`
- Go: 1.22+ (toolchain 1.23.6)
- Key deps: gocui, docker/docker, logrus
