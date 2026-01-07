# Podman Golang bindings
The Podman Go bindings are a set of functions to allow developers to execute Podman operations from within their Go based application. The Go bindings
connect to a Podman service which can run locally or on a remote machine. You can perform many operations including pulling and listing images, starting,
stopping or inspecting containers. Currently, the Podman repository has bindings available for operations on images, containers, pods,
networks and manifests among others.

## Quick Start
The bindings require that the Podman system service is running for the specified user.  This can be done with systemd using the `systemctl` command or manually
by calling the service directly.

### Starting the service with system
The command to start the Podman service differs slightly depending on the user that is running the service.  For a rootful service,
start the service like this:
```
# systemctl start podman.socket
```
For a non-privileged, aka rootless, user, start the service like this:

```
$ systemctl start --user podman.socket
```

### Starting the service manually
It can be handy to run the system service manually.  Doing so allows you to enable debug messaging.
```
$ podman --log-level=debug system service -t0
```
If you do not provide a specific path for the socket, a default is provided.  The location of that socket for
rootful connections is `/run/podman/podman.sock` and for rootless it is `/run/user/USERID#/podman/podman.sock`. For more
information about the Podman system service, see `man podman-system-service`.

### Creating a connection
Ensure the [required dependencies](https://podman.io/getting-started/installation#build-and-run-dependencies) are installed,
as they will be required to compile a Go program making use of the bindings.


The first step for using the bindings is to create a connection to the socket.  As mentioned earlier, the destination
of the socket depends on the user who owns it. In this case, a rootful connection is made.

```
import (
	"context"
	"fmt"
	"os"

	"github.com/containers/podman/v5/pkg/bindings"
)

func main() {
	conn, err := bindings.NewConnection(context.Background(), "unix:///run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}
```
The `conn` variable returned from the `bindings.NewConnection` function can then be used in subsequent function calls
to interact with containers.

### Examples
The following examples build upon the connection example from above.  They are all rootful connections as well.

Note: Optional arguments to the bindings methods are set using With*() methods on *Option structures.
Composite types are not duplicated rather the address is used. As such, you should not change an underlying
field between initializing the *Option structure and calling the bindings method.

#### Inspect a container
The following example obtains the inspect information for a container named `foorbar` and then prints
the container's ID. Note the use of optional inspect options for size.
```
import (
	"context"
	"fmt"
	"os"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
)

func main() {
	conn, err := bindings.NewConnection(context.Background(), "unix:///run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	inspectData, err := containers.Inspect(conn, "foobar", new(containers.InspectOptions).WithSize(true))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// Print the container ID
	fmt.Println(inspectData.ID)
}
```

#### Pull an image
The following example pulls the image `quay.ioo/libpod/alpine_nginx` to the local image store.
```
import (
	"context"
	"fmt"
	"os"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/images"
)

func main() {
	conn, err := bindings.NewConnection(context.Background(), "unix:///run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_, err = images.Pull(conn, "quay.io/libpod/alpine_nginx", nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

```

#### Pull an image, create a container, and start the container
The following example pulls the `quay.io/libpod/alpine_nginx` image and then creates a container named `foobar`
from it.  And finally, it starts the container.
```
import (
	"context"
	"fmt"
	"os"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/specgen"
)

func main() {
	conn, err := bindings.NewConnection(context.Background(), "unix:///run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_, err = images.Pull(conn, "quay.io/libpod/alpine_nginx", nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	s := specgen.NewSpecGenerator("quay.io/libpod/alpine_nginx", false)
	s.Name = "foobar"
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("Container created.")
	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("Container started.")
}
```

## Debugging tips <a name="debugging-tips"></a>

To debug in a development setup, you can start the Podman system service
in debug mode like:

```bash
$ podman --log-level=debug system service -t 0
```

The `--log-level=debug` echoes all the logged requests and is useful to
trace the execution path at a finer granularity. A snippet of a sample run looks like:

```bash
INFO[0000] podman filtering at log level debug
DEBU[0000] Called service.PersistentPreRunE(podman --log-level=debug system service -t0)
DEBU[0000] Ignoring libpod.conf EventsLogger setting "/home/lsm5/.config/containers/containers.conf". Use "journald" if you want to change this setting and remove libpod.conf files.
DEBU[0000] Reading configuration file "/usr/share/containers/containers.conf"
DEBU[0000] Merged system config "/usr/share/containers/containers.conf": {Editors note: the remainder of this line was removed due to Jekyll formatting errors.}
DEBU[0000] Using conmon: "/usr/bin/conmon"
DEBU[0000] Initializing boltdb state at /home/lsm5/.local/share/containers/storage/libpod/bolt_state.db
DEBU[0000] Overriding run root "/run/user/1000/containers" with "/run/user/1000" from database
DEBU[0000] Using graph driver overlay
DEBU[0000] Using graph root /home/lsm5/.local/share/containers/storage
DEBU[0000] Using run root /run/user/1000
DEBU[0000] Using static dir /home/lsm5/.local/share/containers/storage/libpod
DEBU[0000] Using tmp dir /run/user/1000/libpod/tmp
DEBU[0000] Using volume path /home/lsm5/.local/share/containers/storage/volumes
DEBU[0000] Set libpod namespace to ""
DEBU[0000] Not configuring container store
DEBU[0000] Initializing event backend file
DEBU[0000] using runtime "/usr/bin/runc"
DEBU[0000] using runtime "/usr/bin/crun"
WARN[0000] Error initializing configured OCI runtime kata: no valid executable found for OCI runtime kata: invalid argument
DEBU[0000] using runtime "/usr/bin/crun"
INFO[0000] Setting parallel job count to 25
INFO[0000] podman filtering at log level debug
DEBU[0000] Called service.PersistentPreRunE(podman --log-level=debug system service -t0)
DEBU[0000] Ignoring libpod.conf EventsLogger setting "/home/lsm5/.config/containers/containers.conf". Use "journald" if you want to change this setting and remove libpod.conf files.
DEBU[0000] Reading configuration file "/usr/share/containers/containers.conf"
```

If the Podman system service has been started via systemd socket activation,
you can view the logs using journalctl. The logs after a sample run look like:

```bash
$ journalctl --user --no-pager -u podman.socket
-- Reboot --
Jul 22 13:50:40 nagato.nanadai.me systemd[1048]: Listening on Podman API Socket.
$
```

```bash
$ journalctl --user --no-pager -u podman.service
Jul 22 13:50:53 nagato.nanadai.me systemd[1048]: Starting Podman API Service...
Jul 22 13:50:54 nagato.nanadai.me podman[1527]: time="2020-07-22T13:50:54-04:00" level=error msg="Error refreshing volume 38480630a8bdaa3e1a0ebd34c94038591b0d7ad994b37be5b4f2072bb6ef0879: error acquiring lock 0 for volume 38480630a8bdaa3e1a0ebd34c94038591b0d7ad994b37be5b4f2072bb6ef0879: file exists"
Jul 22 13:50:54 nagato.nanadai.me podman[1527]: time="2020-07-22T13:50:54-04:00" level=error msg="Error refreshing volume 47d410af4d762a0cc456a89e58f759937146fa3be32b5e95a698a1d4069f4024: error acquiring lock 0 for volume 47d410af4d762a0cc456a89e58f759937146fa3be32b5e95a698a1d4069f4024: file exists"
Jul 22 13:50:54 nagato.nanadai.me podman[1527]: time="2020-07-22T13:50:54-04:00" level=error msg="Error refreshing volume 86e73f082e344dad38c8792fb86b2017c4f133f2a8db87f239d1d28a78cf0868: error acquiring lock 0 for volume 86e73f082e344dad38c8792fb86b2017c4f133f2a8db87f239d1d28a78cf0868: file exists"
Jul 22 13:50:54 nagato.nanadai.me podman[1527]: time="2020-07-22T13:50:54-04:00" level=error msg="Error refreshing volume 9a16ea764be490a5563e384d9074ab0495e4d9119be380c664037d6cf1215631: error acquiring lock 0 for volume 9a16ea764be490a5563e384d9074ab0495e4d9119be380c664037d6cf1215631: file exists"
Jul 22 13:50:54 nagato.nanadai.me podman[1527]: time="2020-07-22T13:50:54-04:00" level=error msg="Error refreshing volume bfd6b2a97217f8655add13e0ad3f6b8e1c79bc1519b7a1e15361a107ccf57fc0: error acquiring lock 0 for volume bfd6b2a97217f8655add13e0ad3f6b8e1c79bc1519b7a1e15361a107ccf57fc0: file exists"
Jul 22 13:50:54 nagato.nanadai.me podman[1527]: time="2020-07-22T13:50:54-04:00" level=error msg="Error refreshing volume f9b9f630982452ebcbed24bd229b142fbeecd5d4c85791fca440b21d56fef563: error acquiring lock 0 for volume f9b9f630982452ebcbed24bd229b142fbeecd5d4c85791fca440b21d56fef563: file exists"
Jul 22 13:50:54 nagato.nanadai.me podman[1527]: Trying to pull registry.fedoraproject.org/fedora:latest...
Jul 22 13:50:55 nagato.nanadai.me podman[1527]: Getting image source signatures
Jul 22 13:50:55 nagato.nanadai.me podman[1527]: Copying blob sha256:dd9f43919ba05f05d4f783c31e83e5e776c4f5d29dd72b9ec5056b9576c10053
Jul 22 13:50:55 nagato.nanadai.me podman[1527]: Copying config sha256:00ff39a8bf19f810a7e641f7eb3ddc47635913a19c4996debd91fafb6b379069
Jul 22 13:50:55 nagato.nanadai.me podman[1527]: Writing manifest to image destination
Jul 22 13:50:55 nagato.nanadai.me podman[1527]: Storing signatures
Jul 22 13:50:55 nagato.nanadai.me systemd[1048]: podman.service: unit configures an IP firewall, but not running as root.
Jul 22 13:50:55 nagato.nanadai.me systemd[1048]: (This warning is only shown for the first unit using IP firewalling.)
Jul 22 13:51:15 nagato.nanadai.me systemd[1048]: podman.service: Succeeded.
Jul 22 13:51:15 nagato.nanadai.me systemd[1048]: Finished Podman API Service.
Jul 22 13:51:15 nagato.nanadai.me systemd[1048]: podman.service: Consumed 1.339s CPU time.
$
```

You can also verify that the information being passed back and forth is correct by putting
with a tool like `socat`, which can dump what the socket is seeing.

## Reducing Binary Size with "remote" Build Tag
When building a program that uses the Podman Go bindings, you can reduce the binary size by passing the "remote" build tag to the go build command.  This tag excludes code related to local Podman operations, which is not needed for applications that only interact with Podman over a network.
