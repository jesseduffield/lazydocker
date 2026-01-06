# PLAN.md - lazypodman Conversion Plan

This document outlines the plan to convert lazydocker to lazypodman with native libpod support.

## Goals

1. Replace Docker SDK with Podman's native Go library (libpod bindings)
2. Support both socket and socket-less operation
3. Remove Docker-specific commands and features
4. Rename all occurrences of "lazydocker" to "lazypodman"
5. Update documentation to reflect the fork

---

## Phase 1: Rename lazydocker to lazypodman

### 1.1 Go Module Rename

**File:** `go.mod`
- Change module from `github.com/jesseduffield/lazydocker` to `github.com/jesseduffield/lazypodman` (or your own namespace)

### 1.2 Update All Import Statements

**Files to update (49 Go files):**
- `main.go`
- `pkg/app/app.go`
- `pkg/commands/*.go` (all files)
- `pkg/gui/*.go` (all files)
- `pkg/gui/panels/*.go`
- `pkg/gui/presentation/*.go`
- `pkg/config/*.go`
- `pkg/i18n/*.go`
- `pkg/log/log.go`
- `pkg/tasks/tasks.go`
- `pkg/utils/utils.go`
- `pkg/cheatsheet/*.go`
- `scripts/cheatsheet/main.go`
- `scripts/translations/get_required_translations.go`

### 1.3 Update Binary Name References

**Files:**
- `main.go` - Line 47: `flaggy.SetName("lazydocker")` → `"lazypodman"`
- `main.go` - Line 48: Update description
- `main.go` - Line 74: `config.NewAppConfig("lazydocker", ...)` → `"lazypodman"`
- `.goreleaser.yml` - Update binary name and homebrew tap

### 1.4 Update Configuration Paths

**File:** `pkg/config/app_config.go`
- Line ~180: Config directory `"jesseduffield"` and `"lazydocker"` references
- Update for all platforms (Linux, macOS, Windows)

### 1.5 Update Internationalization Strings

**Files (9 language files):**
- `pkg/i18n/english.go`
- `pkg/i18n/french.go`
- `pkg/i18n/german.go`
- `pkg/i18n/spanish.go`
- `pkg/i18n/portuguese.go`
- `pkg/i18n/polish.go`
- `pkg/i18n/dutch.go`
- `pkg/i18n/turkish.go`
- `pkg/i18n/chinese.go`

Replace all "lazydocker" strings with "lazypodman".

### 1.6 Update Scripts and CI/CD

**Files:**
- `scripts/install_update_linux.sh`
- `.circleci/update_docs.sh`
- `.github/workflows/sponsors.yml`
- `.devcontainer/devcontainer.json`
- `Dockerfile`
- `docker-compose.yml`

---

## Phase 2: Replace Docker SDK with Libpod Bindings

### 2.1 Update Dependencies

**File:** `go.mod`

Remove:
```
github.com/docker/cli v27.1.1+incompatible
github.com/docker/docker v28.5.2+incompatible
```

Add (both needed for hybrid approach):
```
github.com/containers/podman/v5/pkg/bindings      // Socket mode (stable API)
github.com/containers/podman/v5/libpod            // Socket-less mode (unstable API)
```

### 2.2 Create Runtime Abstraction (Hybrid Approach)

Create a `ContainerRuntime` interface that supports both socket mode (`pkg/bindings`) and socket-less mode (`libpod`).

**File:** `pkg/commands/runtime.go` (new file)

```go
package commands

type ContainerRuntime interface {
    ListContainers() ([]*Container, error)
    GetContainer(id string) (*Container, error)
    StartContainer(id string) error
    StopContainer(id string) error
    // ... see Phase 3.2 for full interface
    Close() error
}
```

**File:** `pkg/commands/podman.go` (new file, replaces docker.go)

```go
type PodmanCommand struct {
    Runtime   ContainerRuntime  // Either SocketRuntime or LibpodRuntime
    OSCommand *OSCommand
    Config    *config.AppConfig
}

func NewPodmanCommand(...) (*PodmanCommand, error) {
    // Auto-detect: try socket first, fall back to libpod
    // See Phase 3.5 for implementation details
}
```

### 2.3 Two Runtime Implementations

**Socket Mode:** `pkg/commands/runtime_socket.go` - Uses `pkg/bindings` (stable API)
**Socket-less Mode:** `pkg/commands/runtime_libpod.go` - Uses `libpod` directly (unstable API)

See Phase 3.3 and 3.4 for detailed implementation.

### 2.4 Files to Modify

**Primary changes:**
- `pkg/commands/docker.go` → rename to `podman.go`, rewrite client
- `pkg/commands/container.go` - Update all Docker client calls
- `pkg/commands/image.go` - Update all Docker client calls
- `pkg/commands/volume.go` - Update all Docker client calls
- `pkg/commands/network.go` - Update all Docker client calls
- `pkg/commands/container_stats.go` - Update stats streaming

**Socket path updates:**
- `pkg/commands/docker_host_unix.go` → `podman_host_unix.go`
  - Change default: `unix:///run/podman/podman.sock` (rootful)
  - Add rootless: `unix:///run/user/$(id -u)/podman/podman.sock`
- `pkg/commands/docker_host_windows.go` → `podman_host_windows.go`

### 2.5 Update Type References

Replace Docker types with Podman equivalents:
- `container.Summary` → Podman's `entities.ListContainer`
- `container.InspectResponse` → Podman's `define.InspectContainerData`
- `image.Summary` → Podman's `entities.ImageSummary`
- `volume.Volume` → Podman's `entities.VolumeConfigResponse`
- `network.Inspect` → Podman's network types

---

## Phase 3: Hybrid Implementation (Socket + Libpod)

Use a hybrid approach:
- **Socket available** → Use `pkg/bindings` (stable REST API)
- **No socket** → Use `libpod` directly (not CLI fallback)

### 3.1 Dependencies

**Add (both needed):**
```go
github.com/containers/podman/v5/pkg/bindings           // For socket mode
github.com/containers/podman/v5/pkg/bindings/containers
github.com/containers/podman/v5/pkg/bindings/images
github.com/containers/podman/v5/pkg/bindings/volumes
github.com/containers/podman/v5/pkg/bindings/networks

github.com/containers/podman/v5/libpod                 // For socket-less mode
github.com/containers/podman/v5/libpod/define
```

### 3.2 Runtime Interface

**File:** `pkg/commands/runtime.go`

```go
package commands

type ContainerRuntime interface {
    // Container operations
    ListContainers() ([]*Container, error)
    GetContainer(id string) (*Container, error)
    StartContainer(id string) error
    StopContainer(id string) error
    PauseContainer(id string) error
    UnpauseContainer(id string) error
    RestartContainer(id string) error
    RemoveContainer(id string, force bool) error
    PruneContainers() error

    // Image operations
    ListImages() ([]*Image, error)
    RemoveImage(id string) error
    PruneImages() error

    // Volume operations
    ListVolumes() ([]*Volume, error)
    RemoveVolume(name string) error
    PruneVolumes() error

    // Network operations
    ListNetworks() ([]*Network, error)
    RemoveNetwork(name string) error
    PruneNetworks() error

    // Lifecycle
    Close() error
}
```

### 3.3 Socket Mode Implementation

**File:** `pkg/commands/runtime_socket.go`

```go
type SocketRuntime struct {
    conn context.Context
}

func NewSocketRuntime(socketPath string) (*SocketRuntime, error) {
    conn, err := bindings.NewConnection(context.Background(), socketPath)
    if err != nil {
        return nil, err
    }
    return &SocketRuntime{conn: conn}, nil
}
```

### 3.4 Socket-less Mode Implementation

**File:** `pkg/commands/runtime_libpod.go`

```go
type LibpodRuntime struct {
    runtime *libpod.Runtime
}

func NewLibpodRuntime() (*LibpodRuntime, error) {
    runtime, err := libpod.NewRuntime(context.Background())
    if err != nil {
        return nil, err
    }
    return &LibpodRuntime{runtime: runtime}, nil
}
```

### 3.5 Auto-Detection Logic

**File:** `pkg/commands/podman.go`

```go
type PodmanCommand struct {
    Runtime   ContainerRuntime  // Either SocketRuntime or LibpodRuntime
    OSCommand *OSCommand
    Config    *config.AppConfig
}

func NewPodmanCommand(cfg *config.AppConfig, osCommand *OSCommand) (*PodmanCommand, error) {
    var runtime ContainerRuntime

    // Try socket first
    socketPath := detectSocketPath()
    if socketPath != "" && socketExists(socketPath) {
        runtime, _ = NewSocketRuntime(socketPath)
    }

    // Fall back to libpod if socket unavailable
    if runtime == nil {
        runtime, _ = NewLibpodRuntime()
    }

    return &PodmanCommand{Runtime: runtime, ...}, nil
}

func detectSocketPath() string {
    // 1. CONTAINER_HOST env var
    // 2. Rootless: /run/user/{uid}/podman/podman.sock
    // 3. Rootful: /run/podman/podman.sock
}
```

### 3.6 API Method Mappings

**Socket Mode (pkg/bindings):**

| Docker SDK | Podman bindings |
|------------|-----------------|
| `Client.ContainerList()` | `containers.List(conn, opts)` |
| `Client.ContainerStop(id)` | `containers.Stop(conn, id, opts)` |
| `Client.ImageList()` | `images.List(conn, opts)` |
| `Client.VolumeList()` | `volumes.List(conn, opts)` |

**Socket-less Mode (libpod):**

| Docker SDK | Libpod Direct |
|------------|---------------|
| `Client.ContainerList()` | `runtime.GetAllContainers()` |
| `Client.ContainerStop(id)` | `ctr.Stop()` |
| `Client.ImageList()` | `runtime.ImageRuntime().GetImages()` |
| `Client.VolumeList()` | `runtime.GetAllVolumes()` |

### 3.7 Keep Socket Detection Files (Renamed)

- `pkg/commands/docker_host_unix.go` → `pkg/commands/podman_host_unix.go`
- `pkg/commands/docker_host_windows.go` → `pkg/commands/podman_host_windows.go`

Update to detect Podman socket paths instead of Docker.

### 3.8 Caveats

1. **API Stability**: Socket mode (pkg/bindings) is stable; libpod is unstable
2. **Remote Podman**: Only socket mode supports remote connections
3. **Rootless/Rootful**: Both modes handle this automatically

---

## Phase 4: Remove Docker-Specific Commands

### 4.1 Update Command Templates

**File:** `pkg/config/app_config.go`

Change defaults from Docker Compose to Podman Compose:
```go
CommandTemplatesConfig{
    DockerCompose:         "podman-compose",  // was "docker compose"
    RestartService:        "{{ .DockerCompose }} restart {{ .Service.Name }}",
    // ... all other templates stay same, just different default
}
```

### 4.2 Update Compose Detection

**File:** `pkg/commands/docker.go` (now `podman.go`)

Change compose detection:
```go
// Before
err := c.OSCommand.RunCommand("docker compose version")

// After
err := c.OSCommand.RunCommand("podman-compose version")
```

### 4.3 Update Container Labels

**File:** `pkg/commands/docker.go`

Docker Compose labels to check for Podman Compose compatibility:
- `"com.docker.compose.service"` - May need Podman equivalent
- `"com.docker.compose.project"` - May need Podman equivalent

### 4.4 Remove Docker-Specific Features

- Remove `DOCKER_HOST` environment variable handling (replace with `CONTAINER_HOST` or Podman equivalent)
- Remove `DOCKER_CONTEXT` handling
- Update SSH tunneling for Podman

### 4.5 Update Hardcoded Commands

**File:** `pkg/gui/containers_panel.go` (line ~454)
- Change `docker exec` template to `podman exec`

---

## Phase 5: Update Documentation

### 5.1 Update README.md

- Change project name and description
- Add "Forked from lazydocker" attribution
- Update installation instructions for lazypodman
- Update usage examples with podman
- Update screenshots if needed
- Update badges and links

### 5.2 Update Other Documentation

**Files:**
- `CONTRIBUTING.md` - Update repository references
- `docs/Config.md` - Update configuration examples
- `docs/keybindings/*.md` (8 files) - Update any lazydocker references

### 5.3 Update CLAUDE.md

- Already created, update as implementation progresses

---

## Phase 6: Testing and Validation

### 6.1 Update Tests

- Update test fixtures in `test/` directory
- Ensure all existing tests pass with Podman
- Add Podman-specific tests

### 6.2 Integration Testing

- Test with rootful Podman
- Test with rootless Podman
- Test socket mode
- Test CLI fallback mode
- Test with podman-compose

### 6.3 Platform Testing

- Linux (primary platform)
- macOS (via Podman machine)
- Windows (via Podman machine)

---

## File Summary

### Files to Rename
| Original | New |
|----------|-----|
| `pkg/commands/docker.go` | `pkg/commands/podman.go` |
| `pkg/commands/docker_host_unix.go` | `pkg/commands/podman_host_unix.go` |
| `pkg/commands/docker_host_windows.go` | `pkg/commands/podman_host_windows.go` |

### Files with Major Changes
- `go.mod` - Module rename + dependency swap
- `main.go` - Binary name, description
- `pkg/commands/podman.go` - Complete rewrite
- `pkg/commands/container.go` - API migration
- `pkg/commands/image.go` - API migration
- `pkg/commands/volume.go` - API migration
- `pkg/commands/network.go` - API migration
- `pkg/config/app_config.go` - Config paths + new options
- `README.md` - Complete rewrite

### Files with String Replacements Only
- All 9 i18n files
- All GUI files (import paths)
- All presentation files (import paths)
- Scripts and CI files
- Documentation files

---

## Estimated Scope

- **~80 files** need modification
- **~50 files** need import path updates only
- **~10 files** need significant code changes
- **~20 files** need content/string updates
