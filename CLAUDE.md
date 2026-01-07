# CLAUDE.md - lazypodman

## Project Overview

This is a fork of **lazydocker** converted to **lazypodman** - a terminal UI for managing Podman containers.

**Goals:**
1. Support native libpod (Podman's Go library) instead of Docker SDK
2. Enable socket-less operation for Podman commands (no daemon required)

**Current State:** Conversion complete. The codebase now uses Podman bindings (`github.com/containers/podman/v5` v5.7.1) with a hybrid runtime architecture supporting both socket mode (REST API) and socket-less mode (direct libpod).

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
./lazypodman -f podman-compose.yml -f podman-compose.override.yml
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

### Key Files

**Runtime abstraction layer:**
- `pkg/commands/runtime.go` - `ContainerRuntime` interface abstracting all container operations
- `pkg/commands/runtime_socket.go` - Socket mode implementation using Podman REST API bindings
- `pkg/commands/runtime_libpod.go` - Direct libpod implementation (Linux+CGO only)
- `pkg/commands/runtime_libpod_stub.go` - Stub for non-Linux platforms
- `pkg/commands/runtime_types.go` - Custom types (ContainerSummary, ImageSummary, PodSummary, etc.)

**Podman integration:**
- `pkg/commands/podman.go` - Main client connection, auto-detection, and initialization
- `pkg/commands/container.go` - Container wrapper operations
- `pkg/commands/pod.go` - Pod wrapper with state and container count methods
- `pkg/commands/container_list_item.go` - Unified wrapper for pods/containers in list view
- `pkg/commands/image.go` - Image wrapper operations
- `pkg/commands/volume.go` - Volume wrapper operations
- `pkg/commands/network.go` - Network wrapper operations
- `pkg/commands/service.go` - Compose service operations
- `pkg/commands/podman_host_unix.go` - Unix socket detection
- `pkg/commands/podman_host_windows.go` - Windows pipe detection

**Application entry:**
- `main.go` - Entry point, CLI flags
- `pkg/app/app.go` - App struct, initialization flow

## Runtime Architecture

### Hybrid Runtime System

The codebase uses a `ContainerRuntime` interface with two implementations:

1. **Socket Mode** (`runtime_socket.go`)
   - Uses Podman REST API via `pkg/bindings`
   - Connects to local or remote Podman instances
   - Supports SSH tunneling for remote hosts
   - Real event streaming

2. **Libpod Mode** (`runtime_libpod.go`)
   - Direct libpod library calls (no socket required)
   - Linux + CGO only
   - Event polling (2-second intervals)
   - Fallback when socket unavailable

### Connection Flow
1. `NewPodmanCommand()` in `podman.go` initializes runtime
2. Host determined from `CONTAINER_HOST` env or platform defaults
3. Tries socket mode first, falls back to libpod if unavailable
4. Auto-detects compose tool: `podman-compose`, `podman compose`, or `docker-compose`

### Runtime Interface Methods
```go
// Container operations
ListContainers() / InspectContainer() / ContainerStats()
StartContainer() / StopContainer() / PauseContainer() / UnpauseContainer()
RestartContainer() / RemoveContainer() / PruneContainers() / ContainerTop()

// Pod operations
ListPods() - Returns all pods with their metadata

// Image operations
ListImages() / InspectImage() / ImageHistory() / RemoveImage() / PruneImages()

// Volume/Network
ListVolumes() / RemoveVolume() / PruneVolumes()
ListNetworks() / RemoveNetwork() / PruneNetworks()

// Events
GetEvents() - Streaming (socket) or polling (libpod)
```

### Command Execution Patterns
1. **Runtime interface calls** - For most container/image operations
2. **Shell commands** - For interactive operations (attach, logs) and compose
3. **Template-based** - Configurable command templates in config

## Platform Support

| Platform | Socket Mode | Libpod Mode |
|----------|-------------|-------------|
| Linux    | ✅          | ✅ (requires CGO) |
| macOS    | ✅          | ❌ (stub)    |
| Windows  | ✅          | ❌ (stub)    |

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
- Key deps: gocui, containers/podman/v5, logrus

## Known Limitations

- **Network Prune**: Libpod mode has a TODO for manual pruning implementation
- **Event Streaming**: Libpod mode uses polling (2-second intervals) instead of true event streaming
- **Libpod Availability**: Socket-less mode requires Linux + CGO compilation
- **Docker SDK**: Present in go.mod as indirect transitive dependency (pulled by Podman itself, not used by application code)
