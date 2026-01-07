# Docker volume extension api.

Go handler to create external volume extensions for Docker.

## Usage

This library is designed to be integrated in your program.

1. Implement the `volume.Driver` interface.
2. Initialize a `volume.Handler` with your implementation.
3. Call either `ServeTCP` or `ServeUnix` from the `volume.Handler`.

### Example using TCP sockets:

```go
  d := MyVolumeDriver{}
  h := volume.NewHandler(d)
  h.ServeTCP("test_volume", ":8080")
```

### Example using Unix sockets:

```go
  d := MyVolumeDriver{}
  h := volume.NewHandler(d)
  u, _ := user.Lookup("root")
  gid, _ := strconv.Atoi(u.Gid)
  h.ServeUnix("test_volume", gid)
```

## Full example plugins

- https://github.com/calavera/docker-volume-glusterfs
- https://github.com/calavera/docker-volume-keywhiz
- https://github.com/quobyte/docker-volume
- https://github.com/NimbleStorage/Nemo
