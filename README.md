# lazyapple

A terminal UI for [Apple Container](https://developer.apple.com/documentation/virtualization/running_linux_containers) -- the native macOS container runtime introduced in macOS 26.

This is a fork of [lazydocker](https://github.com/jesseduffield/lazydocker) by Jesse Duffield, rewritten to work with Apple's `container` CLI instead of Docker.

## What is this?

Apple introduced a native container runtime for macOS. It ships a CLI tool called `container` that can run Linux containers without Docker. **lazyapple** gives you a terminal UI to manage those containers, just like lazydocker does for Docker.

### What works

- **Containers**: list, start, stop, restart, kill, remove, view logs, exec shell, view stats/env/config/top
- **Images**: list, pull, remove, prune
- **Volumes**: list, remove, prune
- **Networks**: list, remove, prune

### What doesn't exist (because Apple Container doesn't support it)

- No docker-compose / services / projects
- No pause/unpause
- No attach (use exec shell instead)
- No events streaming (polling is used)
- No `--since` or `--timestamps` on logs

## Requirements

- **macOS 26** (Tahoe) or later
- Apple Container CLI installed (`container` command available in PATH)

Verify it works:

```
container list --all
```

If you get `command not found`, Apple Container is not installed on your system.

## Install

### Build from source

```bash
git clone https://github.com/g-battaglia/lazy-apple-container.git
cd lazy-apple-container
go build -o lazyapple .
```

### Run it

```bash
./lazyapple
```

### Optional: install to PATH

```bash
sudo cp lazyapple /usr/local/bin/
```

Then just run `lazyapple` from anywhere.

## Keybindings

### Global

| Key | Action |
|-----|--------|
| `q` / `Ctrl+C` | Quit |
| `1` | Containers |
| `2` | Images |
| `3` | Volumes |
| `4` | Networks |
| `x` / `?` | Menu |
| `Enter` | Focus main panel |
| `PgUp` / `PgDn` | Scroll |
| `[` / `]` | Prev/next tab |
| `/` | Filter list |

### Containers

| Key | Action |
|-----|--------|
| `s` | Stop |
| `r` | Restart (stop + start) |
| `k` | Kill |
| `d` | Remove |
| `E` | Exec shell |
| `a` | Attach (exec shell) |
| `m` | View logs in terminal |
| `e` | Toggle stopped containers |
| `w` | Open in browser |

### Images / Volumes / Networks

| Key | Action |
|-----|--------|
| `d` | Remove |
| `b` | Bulk commands (prune, etc.) |

## Config

Config lives at `~/.config/lazyapple/config.yml`.

```yaml
gui:
  scrollHeight: 2
  sidePanelWidth: 0.333
  theme:
    activeBorderColor:
      - green
      - bold
logs:
  tail: "100"
stats:
  graphs:
    - caption: CPU (%)
      statPath: DerivedStats.CPUPercentage
      color: blue
    - caption: Memory (%)
      statPath: DerivedStats.MemoryPercentage
      color: green
```

## How it differs from lazydocker

| | lazydocker | lazyapple |
|---|---|---|
| Runtime | Docker Engine | Apple Container |
| CLI | `docker` | `container` |
| Compose | Yes | No |
| Services/Projects panels | Yes | No |
| Pause/Unpause | Yes | No |
| Events API | Yes (streaming) | No (polling) |
| Platform | Linux, macOS, Windows | macOS 26+ only |

## Credits

Fork of [lazydocker](https://github.com/jesseduffield/lazydocker) by [Jesse Duffield](https://github.com/jesseduffield). All the TUI framework, panel system, and keybinding infrastructure comes from the original project.

## License

MIT
