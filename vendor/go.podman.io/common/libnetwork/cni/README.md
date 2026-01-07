This package abstracts CNI from libpod.
It implements the `ContainerNetwork` interface defined in [libpod/network/types/network.go](../types/network.go) for the CNI backend.


## Testing
Run the tests with:
```
go test -v -mod=vendor -cover ./libpod/network/cni/
```
Run the tests as root to also test setup/teardown. This will execute CNI and therefore the cni plugins have to be installed.
