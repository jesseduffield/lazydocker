![buildah logo (light)](logos/buildah-logo_large.png#gh-light-mode-only)
![buildah logo (dark)](logos/buildah-logo_reverse_large.png#gh-dark-mode-only)

# [Buildah](https://www.youtube.com/embed/YVk5NgSiUw8) - a tool that facilitates building [Open Container Initiative (OCI)](https://www.opencontainers.org/) container images

[![Go Report Card](https://goreportcard.com/badge/github.com/containers/buildah)](https://goreportcard.com/report/github.com/containers/buildah)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/10579/badge)](https://www.bestpractices.dev/projects/10579)


The Buildah package provides a command line tool that can be used to
* create a working container, either from scratch or using an image as a starting point
* create an image, either from a working container or via the instructions in a Dockerfile
* images can be built in either the OCI image format or the traditional upstream docker image format
* mount a working container's root filesystem for manipulation
* unmount a working container's root filesystem
* use the updated contents of a container's root filesystem as a filesystem layer to create a new image
* delete a working container or an image
* rename a local container

## Buildah Information for Developers

For blogs, release announcements and more, please checkout the [buildah.io](https://buildah.io) website!

**[Buildah Container Images](https://github.com/containers/image_build/blob/main/buildah/README.md)**

**[Buildah Demos](demos)**

**[Changelog](CHANGELOG.md)**

**[Contributing](CONTRIBUTING.md)**

**[Development Plan](developmentplan.md)**

**[Installation notes](install.md)**

**[Troubleshooting Guide](troubleshooting.md)**

**[Tutorials](docs/tutorials)**

## Buildah and Podman relationship

Buildah and Podman are two complementary open-source projects that are
available on most Linux platforms and both projects reside at
[GitHub.com](https://github.com) with Buildah
[here](https://github.com/containers/buildah) and Podman
[here](https://github.com/containers/podman).  Both, Buildah and Podman are
command line tools that work on Open Container Initiative (OCI) images and
containers.  The two projects differentiate in their specialization.

Buildah specializes in building OCI images.  Buildah's commands replicate all
of the commands that are found in a Dockerfile.  This allows building images
with and without Dockerfiles while not requiring any root privileges.
Buildah’s ultimate goal is to provide a lower-level coreutils interface to
build images.  The flexibility of building images without Dockerfiles allows
for the integration of other scripting languages into the build process.
Buildah follows a simple fork-exec model and does not run as a daemon
but it is based on a comprehensive API in golang, which can be vendored
into other tools.

Podman specializes in all of the commands and functions that help you to maintain and modify
OCI images, such as pulling and tagging.  It also allows you to create, run, and maintain those containers
created from those images.  For building container images via Dockerfiles, Podman uses Buildah's
golang API and can be installed independently from Buildah.

A major difference between Podman and Buildah is their concept of a container.  Podman
allows users to create "traditional containers" where the intent of these containers is
to be long lived.  While Buildah containers are really just created to allow content
to be added back to the container image.  An easy way to think of it is the
`buildah run` command emulates the RUN command in a Dockerfile while the `podman run`
command emulates the `docker run` command in functionality.  Because of this and their underlying
storage differences, you can not see Podman containers from within Buildah or vice versa.

In short, Buildah is an efficient way to create OCI images while Podman allows
you to manage and maintain those images and containers in a production environment using
familiar container cli commands.  For more details, see the
[Container Tools Guide](https://github.com/containers/buildah/tree/main/docs/containertools).

## Example

From [`./examples/lighttpd.sh`](examples/lighttpd.sh):

```bash
$ cat > lighttpd.sh <<"EOF"
#!/usr/bin/env bash

set -x

ctr1=$(buildah from "${1:-fedora}")

## Get all updates and install our minimal httpd server
buildah run "$ctr1" -- dnf update -y
buildah run "$ctr1" -- dnf install -y lighttpd

## Include some buildtime annotations
buildah config --annotation "com.example.build.host=$(uname -n)" "$ctr1"

## Run our server and expose the port
buildah config --cmd "/usr/sbin/lighttpd -D -f /etc/lighttpd/lighttpd.conf" "$ctr1"
buildah config --port 80 "$ctr1"

## Commit this container to an image name
buildah commit "$ctr1" "${2:-$USER/lighttpd}"
EOF

$ chmod +x lighttpd.sh
$ ./lighttpd.sh
```

## Commands
| Command                                              | Description                                                                                          |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| [buildah-add(1)](/docs/buildah-add.1.md)               | Add the contents of a file, URL, or a directory to the container.                                    |
| [buildah-build(1)](/docs/buildah-build.1.md)           | Build an image using instructions from Containerfiles or Dockerfiles.                                |
| [buildah-commit(1)](/docs/buildah-commit.1.md)         | Create an image from a working container.                                                            |
| [buildah-config(1)](/docs/buildah-config.1.md)         | Update image configuration settings.                                                                 |
| [buildah-containers(1)](/docs/buildah-containers.1.md) | List the working containers and their base images.                                                   |
| [buildah-copy(1)](/docs/buildah-copy.1.md)             | Copies the contents of a file, URL, or directory into a container's working directory.               |
| [buildah-from(1)](/docs/buildah-from.1.md)             | Creates a new working container, either from scratch or using a specified image as a starting point. |
| [buildah-images(1)](/docs/buildah-images.1.md)         | List images in local storage.                                                                        |
| [buildah-info(1)](/docs/buildah-info.1.md)             | Display Buildah system information.                                                                  |
| [buildah-inspect(1)](/docs/buildah-inspect.1.md)       | Inspects the configuration of a container or image.                                                  |
| [buildah-mount(1)](/docs/buildah-mount.1.md)           | Mount the working container's root filesystem.                                                       |
| [buildah-pull(1)](/docs/buildah-pull.1.md)             | Pull an image from the specified location.                                                           |
| [buildah-push(1)](/docs/buildah-push.1.md)             | Push an image from local storage to elsewhere.                                                       |
| [buildah-rename(1)](/docs/buildah-rename.1.md)         | Rename a local container.                                                                            |
| [buildah-rm(1)](/docs/buildah-rm.1.md)                 | Removes one or more working containers.                                                              |
| [buildah-rmi(1)](/docs/buildah-rmi.1.md)               | Removes one or more images.                                                                          |
| [buildah-run(1)](/docs/buildah-run.1.md)               | Run a command inside of the container.                                                               |
| [buildah-tag(1)](/docs/buildah-tag.1.md)               | Add an additional name to a local image.                                                             |
| [buildah-umount(1)](/docs/buildah-umount.1.md)         | Unmount a working container's root file system.                                                      |
| [buildah-unshare(1)](/docs/buildah-unshare.1.md)       | Launch a command in a user namespace with modified ID mappings.                                      |
| [buildah-version(1)](/docs/buildah-version.1.md)       | Display the Buildah Version Information                                                              |

**Future goals include:**
* more CI tests
* additional CLI commands (?)
