![buildah logo](https://cdn.rawgit.com/containers/buildah/main/logos/buildah-logo_large.png)

# Installation Instructions

## Installing packaged versions of buildah

### [Arch Linux](https://www.archlinux.org)

```bash
sudo pacman -S buildah
```

### [CentOS](https://www.centos.org)

Buildah is available in the default Extras repos for CentOS 7 and in
the AppStream repo for CentOS 8 and Stream, however the available version often
lags the upstream release.

```bash
sudo yum -y install buildah
```

### [Debian](https://debian.org)

The buildah package is available in
the [Bookworm](https://packages.debian.org/bookworm/buildah), which
is the current stable release (Debian 12), as well as Debian Unstable/Sid.

```bash
# Debian Stable/Bookworm or Unstable/Sid
sudo apt-get update
sudo apt-get -y install buildah
```


### [Fedora](https://www.fedoraproject.org)

```bash
sudo dnf -y install buildah
```

### [Fedora SilverBlue](https://silverblue.fedoraproject.org)

Installed by default

### [Fedora CoreOS](https://coreos.fedoraproject.org)

Not Available.  Must be installed via package layering.

rpm-ostree install buildah

Note: [`podman`](https://podman.io) build is available by default.

### [Gentoo](https://www.gentoo.org)
[app-containers/buildah](https://packages.gentoo.org/packages/app-containers/buildah)
```bash
sudo emerge app-containers/buildah
```

### [openSUSE](https://www.opensuse.org)

```bash
sudo zypper install buildah
```

### [openSUSE Kubic](https://kubic.opensuse.org)

transactional-update pkg in buildah

### [RHEL7](https://www.redhat.com/en/technologies/linux-platforms/enterprise-linux)

Subscribe, then enable Extras channel and install buildah.

```bash
sudo subscription-manager repos --enable=rhel-7-server-extras-rpms
sudo yum -y install buildah
```

#### [Raspberry Pi OS arm64 (beta)](https://downloads.raspberrypi.org/raspios_arm64/images/)

Raspberry Pi OS use the standard Debian's repositories,
so it is fully compatible with Debian's arm64 repository.
You can simply follow the [steps for Debian](#debian) to install buildah.


### [RHEL8 Beta](https://www.redhat.com/en/blog/powering-its-future-while-preserving-present-introducing-red-hat-enterprise-linux-8-beta?intcmp=701f2000001Cz6OAAS)

```bash
sudo yum module enable -y container-tools:1.0
sudo yum module install -y buildah
```

### [Ubuntu](https://www.ubuntu.com)

The buildah package is available in the official repositories for Ubuntu 20.10
and newer.

```bash
# Ubuntu 20.10 and newer
sudo apt-get -y update
sudo apt-get -y install buildah
```

# Building from scratch

## System Requirements

### Kernel Version Requirements
To run Buildah on Red Hat Enterprise Linux or CentOS, version 7.4 or higher is required.
On other Linux distributions Buildah requires a kernel version that supports the OverlayFS and/or fuse-overlayfs filesystem -- you'll need to consult your distribution's documentation to determine a minimum version number.

### runc Requirement

Buildah uses `runc` to run commands when `buildah run` is used, or when `buildah build`
encounters a `RUN` instruction, so you'll also need to build and install a compatible version of
[runc](https://github.com/opencontainers/runc) for Buildah to call for those cases.  If Buildah is installed
via a package manager such as yum, dnf or apt-get, runc will be installed as part of that process.

### CNI Requirement

When Buildah uses `runc` to run commands, it defaults to running those commands
in the host's network namespace.  If the command is being run in a separate
user namespace, though, for example when ID mapping is used, then the command
will also be run in a separate network namespace.

A newly-created network namespace starts with no network interfaces, so
commands which are run in that namespace are effectively disconnected from the
network unless additional setup is done.  Buildah relies on the CNI
[library](https://github.com/containernetworking/cni) and
[plugins](https://github.com/containernetworking/plugins) to set up interfaces
and routing for network namespaces.

If Buildah is installed via a package manager such as yum, dnf or apt-get, a
package containing CNI plugins may be available (in Fedora, the package is
named `containernetworking-cni`).  If not, they will need to be installed,
for example using:
```
  git clone https://github.com/containernetworking/plugins
  ( cd ./plugins; ./build_linux.sh )
  sudo mkdir -p /opt/cni/bin
  sudo install -v ./plugins/bin/* /opt/cni/bin
```

The CNI library needs to be configured so that it will know which plugins to
call to set up namespaces.  Usually, this configuration takes the form of one
or more configuration files in the `/etc/cni/net.d` directory.  A set of example
configuration files is included in the
[`docs/cni-examples`](https://github.com/containers/buildah/tree/main/docs/cni-examples)
directory of this source tree.

## Package Installation

Buildah is available on several software repositories and can be installed via a package manager such
as yum, dnf or apt-get on a number of Linux distributions.

## Installation from GitHub

Prior to installing Buildah, install the following packages on your Linux distro:
* make
* golang (Requires version 1.13 or higher.)
* bats
* btrfs-progs-devel
* bzip2
* git
* go-md2man
* gpgme-devel
* glib2-devel
* libassuan-devel
* libseccomp-devel
* runc (Requires version 1.0 RC4 or higher.)
* containers-common

### Fedora

In Fedora, you can use this command:

```
 dnf -y install \
    make \
    golang \
    bats \
    btrfs-progs-devel \
    glib2-devel \
    gpgme-devel \
    libassuan-devel \
    libseccomp-devel \
    git \
    bzip2 \
    go-md2man \
    runc \
    containers-common
```

Then to install Buildah on Fedora follow the steps in this example:

```
  git clone https://github.com/containers/buildah
  cd buildah
  make
  sudo make install
  buildah --help
```

### RHEL, CentOS

In RHEL and CentOS, run this command to install the build dependencies:

```
 yum -y install \
    make \
    golang \
    bats \
    btrfs-progs-devel \
    glib2-devel \
    gpgme-devel \
    libassuan-devel \
    libseccomp-devel \
    git \
    bzip2 \
    go-md2man \
    runc \
    skopeo-containers
```

The build steps for Buildah on RHEL or CentOS are the same as for Fedora, above.

### openSUSE

On openSUSE Tumbleweed, install go via `zypper in go`, then run this command:

```
 zypper in make \
    git \
    golang \
    runc \
    bzip2 \
    libgpgme-devel \
    libseccomp-devel \
    libbtrfs-devel \
    go-md2man
```

The build steps for Buildah on SUSE / openSUSE are the same as for Fedora, above.


### Ubuntu/Debian

In Ubuntu 22.10 (Karmic) or Debian 12 (Bookworm) you can use these commands:

```
  sudo apt-get -y -qq update
  sudo apt-get -y install bats btrfs-progs git go-md2man golang libapparmor-dev libglib2.0-dev libgpgme11-dev libseccomp-dev libselinux1-dev make runc skopeo libbtrfs-dev
```

The build steps for Buildah on Debian or Ubuntu are the same as for Fedora, above.

## Vendoring - Dependency Management

This project is using [go modules](https://github.com/golang/go/wiki/Modules) for dependency management.  If the CI is complaining about a pull request leaving behind an unclean state, it is very likely right about it.  After changing dependencies, make sure to run `make vendor-in-container` to synchronize the code with the go module and repopulate the `./vendor` directory.

## Configuration files

The following configuration files are required in order for Buildah to run appropriately.  The
majority of these files are commonly contained in the `containers-common` package.

### [registries.conf](https://github.com/containers/buildah/blob/main/docs/samples/registries.conf)

#### Man Page: [registries.conf.5](https://github.com/containers/image/blob/main/docs/containers-registries.conf.5.md)

`/etc/containers/registries.conf`

registries.conf is the configuration file which specifies which container registries should be consulted when completing image names which do not include a registry or domain portion.

#### Example from the Fedora `containers-common` package

```
cat /etc/containers/registries.conf
# This is a system-wide configuration file used to
# keep track of registries for various container backends.
# It adheres to TOML format and does not support recursive
# lists of registries.

# The default location for this configuration file is /etc/containers/registries.conf.

# The only valid categories are: 'registries.search', 'registries.insecure',
# and 'registries.block'.

[registries.search]
registries = ['docker.io', 'registry.fedoraproject.org', 'quay.io', 'registry.access.redhat.com']

# If you need to access insecure registries, add the registry's fully-qualified name.
# An insecure registry is one that does not have a valid SSL certificate or only does HTTP.
[registries.insecure]
registries = []


# If you need to block pull access from a registry, uncomment the section below
# and add the registries fully-qualified name.
#
# Docker only
[registries.block]
registries = []
```

### [mounts.conf](https://src.fedoraproject.org/rpms/skopeo/blob/main/f/mounts.conf)

`/usr/share/containers/mounts.conf` and optionally `/etc/containers/mounts.conf`

The mounts.conf files specify volume mount files or directories that are automatically mounted inside containers when executing the `buildah run` or `buildah build` commands.  Container processes can then use this content.  The volume mount content does not get committed to the final image.  This file is usually provided by the containers-common package.

Usually these directories are used for passing secrets or credentials required by the package software to access remote package repositories.

For example, a mounts.conf with the line "`/usr/share/rhel/secrets:/run/secrets`", the content of `/usr/share/rhel/secrets` directory is mounted on `/run/secrets` inside the container.  This mountpoint allows Red Hat Enterprise Linux subscriptions from the host to be used within the container.  It is also possible to omit the destination if it's equal to the source path.  For example, specifying `/var/lib/secrets` will mount the directory into the same container destination path `/var/lib/secrets`.

Note this is not a volume mount. The content of the volumes is copied into container storage, not bind mounted directly from the host.

#### Example from the Fedora `containers-common` package:

```
cat /usr/share/containers/mounts.conf
/usr/share/rhel/secrets:/run/secrets
```

### [seccomp.json](https://src.fedoraproject.org/rpms/skopeo/blob/main/f/seccomp.json)

`/usr/share/containers/seccomp.json`

seccomp.json contains the list of seccomp rules to be allowed inside of
containers.  This file is usually provided by the containers-common package.

The link above takes you to the seccomp.json

### [policy.json](https://github.com/containers/skopeo/blob/main/default-policy.json)

`/etc/containers/policy.json`

#### Man Page: [policy.json.5](https://github.com/containers/image/blob/main/docs/policy.json.md)


#### Example from the Fedora `containers-common` package:

```
cat /etc/containers/policy.json
{
    "default": [
	{
	    "type": "insecureAcceptAnything"
	}
    ],
    "transports":
	{
	    "docker-daemon":
		{
		    "": [{"type":"insecureAcceptAnything"}]
		}
	}
}
```

## Debug with Delve and the like

To make a source debug build without optimizations use `BUILDDEBUG=1`, like:
```
make all BUILDDEBUG=1
```

## Vendoring

Buildah uses Go Modules for vendoring purposes.  If you need to update or add a vendored package into Buildah, please follow this procedure:
 * Enter into your sandbox `src/github.com/containers/buildah` and ensure that the GOPATH variable is set to the directory prior as noted above.
 * `export GO111MODULE=on`
 * `go get` the needed version:
     * Assuming you want to 'bump' the `github.com/containers/storage` package to version 1.12.13, use this command: `go get github.com/containers/storage@v1.12.13`
     *  Assuming that you want to 'bump' the `github.com/containers/storage` package to a particular commit, use this command: `go get github.com/containers/storage@e307568568533c4afccdf7b56df7b4493e4e9a7b`
 * `make vendor-in-container`
 * `make`
 * `make install`
 * Then add any updated or added files with `git add` then do a `git commit` and create a PR.

### Vendor from your own fork

If you wish to vendor in your personal fork to try changes out (assuming containers/storage in the below example):

 * `go mod edit -replace github.com/containers/storage=github.com/{mygithub_username}/storage@YOUR_BRANCH`
 * `make vendor-in-container`

To revert
 * `go mod edit -dropreplace github.com/containers/storage`
 * `make vendor-in-container`

To speed up fetching dependencies, you can use a [Go Module Proxy](https://proxy.golang.org) by setting `GOPROXY=https://proxy.golang.org`.
