![buildah logo](https://cdn.rawgit.com/containers/buildah/main/logos/buildah-logo_large.png)

# Troubleshooting

## A list of common issues and solutions for Buildah

---
### 1) No such image

When doing a `buildah pull` or `buildah build` command and a "common" image can not be pulled,
it is likely that the `/etc/containers/registries.conf` file is either not installed or possibly
misconfigured.  This issue might also indicate that other required files as listed in the
[Configuration Files](https://github.com/containers/buildah/blob/main/install.md#configuration-files)
section of the Installation Instructions are also not installed.

#### Symptom
```console
$ sudo buildah build -f Dockerfile .
STEP 1: FROM alpine
error creating build container: 2 errors occurred:

* Error determining manifest MIME type for docker://localhost/alpine:latest: pinging docker registry returned: Get https://localhost/v2/: dial tcp [::1]:443: connect: connection refused
* Error determining manifest MIME type for docker://registry.access.redhat.com/alpine:latest: Error reading manifest latest in registry.access.redhat.com/alpine: unknown: Not Found
error building: error creating build container: no such image "alpine" in registry: image not known
```

#### Solution

  * Verify that the `/etc/containers/registries.conf` file exists.  If not, verify that the containers-common package is installed.
  * Verify that the entries in the `[registries.search]` section of the /etc/containers/registries file are valid and reachable.
  * Verify that the image you requested is either fully qualified, or that it exists on one of your search registries.
  * Verify that the image is public or that you have logged in to at least one search registry which contains the private image.
  * Verify that the other required [Configuration Files](https://github.com/containers/buildah/blob/main/install.md#configuration-files) are installed.

---
### 2) http: server gave HTTP response to HTTPS client

When doing a Buildah command such as `build`, `commit`, `from`, or `push` to a registry,
tls verification is turned on by default.  If authentication is not used with
those commands, this error can occur.

#### Symptom
```console
# buildah push alpine docker://localhost:5000/myalpine:latest
Getting image source signatures
Get https://localhost:5000/v2/: http: server gave HTTP response to HTTPS client
```

#### Solution

By default tls verification is turned on when communicating to registries from
Buildah.  If the registry does not require authentication the Buildah commands
such as `build`, `commit`, `from` and `pull` will fail unless tls verification is turned
off using the `--tls-verify` option.  **NOTE:** It is not at all recommended to
communicate with a registry and not use tls verification.

  * Turn off tls verification by passing false to the tls-verification option.
  * I.e. `buildah push --tls-verify=false alpine docker://localhost:5000/myalpine:latest`

---
### 3) `buildah run` command fails with pipe or output redirection

When doing a `buildah run` command while using a pipe ('|') or output redirection ('>>'),
the command will fail, often times with a `command not found` type of error.

#### Symptom
When executing a `buildah run` command with a pipe or output redirection such as the
following commands:

```console
# buildah run $whalecontainer /usr/games/fortune -a | cowsay
# buildah run $newcontainer echo "daemon off;" >> /etc/nginx/nginx.conf
# buildah run $newcontainer echo "nginx on Fedora" > /usr/share/nginx/html/index.html
```
the `buildah run` command will not complete and an error will be raised.

#### Solution
There are two solutions to this problem.  The
[`podman run`](https://github.com/containers/podman/blob/main/docs/podman-run.1.md)
command can be used in place of `buildah run`.  To still use `buildah run`, surround
the command with single quotes and use `bash -c`.  The previous examples would be
changed to:

```console
# buildah run $whalecontainer bash -c '/usr/games/fortune -a | cowsay'
# buildah run $newcontainer bash -c 'echo "daemon off;" >> /etc/nginx/nginx.conf'
# buildah run $newcontainer bash -c 'echo "nginx on Fedora" > /usr/share/nginx/html/index.html'
```

---
### 4) `buildah push alpine oci:~/myalpine:latest` fails with lstat error

When doing a `buildah push` command and the target image has a tilde (`~`) character
in it, an lstat error will be raised stating there is no such file or directory.
This is expected behavior for shell expansion of the tilde character as it is only
expanded at the start of a word.  This behavior is documented
[here](https://www.gnu.org/software/libc/manual/html_node/Tilde-Expansion.html).

#### Symptom
```console
$ sudo pull alpine
$ sudo buildah push alpine oci:~/myalpine:latest
lstat /home/myusername/~: no such file or directory
```

#### Solution

  * Replace `~` with `$HOME` or the fully specified directory `/home/myusername`.
    * `$ sudo buildah push alpine oci:${HOME}/myalpine:latest`


---
### 5) Rootless buildah build fails EPERM on NFS:

NFS enforces file creation on different UIDs on the server side and does not understand user namespace, which rootless Podman requires.  When a container root process like YUM attempts to create a file owned by a different UID, NFS Server denies the creation.  NFS is also a problem for the file locks when the storage is on it.  Other distributed file systems (for example: Lustre, Spectrum Scale, the General Parallel File System (GPFS)) are also not supported when running in rootless mode as these file systems do not understand user namespace.

#### Symptom
```console
$ buildah build .
ERRO[0014] Error while applying layer: ApplyLayer exit status 1 stdout:  stderr: open /root/.bash_logout: permission denied
error creating build container: Error committing the finished image: error adding layer with blob "sha256:a02a4930cb5d36f3290eb84f4bfa30668ef2e9fe3a1fb73ec015fc58b9958b17": ApplyLayer exit status 1 stdout:  stderr: open /root/.bash_logout: permission denied
```

#### Solution
Choose one of the following:
  * Setup containers/storage in a different directory, not on an NFS share.
  * Otherwise just run buildah as root, via `sudo buildah`
---
### 6) Rootless buildah build fails when using OverlayFS:

The Overlay file system (OverlayFS) requires the ability to call the `mknod` command when creating whiteout files
when extracting an image.  However, a rootless user does not have the privileges to use `mknod` in this capacity.

#### Symptom
```console
buildah build --storage-driver overlay .
STEP 1: FROM docker.io/ubuntu:xenial
Getting image source signatures
Copying blob edf72af6d627 done
Copying blob 3e4f86211d23 done
Copying blob 8d3eac894db4 done
Copying blob f7277927d38a done
Copying config 5e13f8dd4c done
Writing manifest to image destination
Storing signatures
Error: error creating build container: Error committing the finished image: error adding layer with blob "sha256:8d3eac894db4dc4154377ad28643dfe6625ff0e54bcfa63e0d04921f1a8ef7f8": Error processing tar file(exit status 1): operation not permitted
$ buildah build .
ERRO[0014] Error while applying layer: ApplyLayer exit status 1 stdout:  stderr: open /root/.bash_logout: permission denied
error creating build container: Error committing the finished image: error adding layer with blob "sha256:a02a4930cb5d36f3290eb84f4bfa30668ef2e9fe3a1fb73ec015fc58b9958b17": ApplyLayer exit status 1 stdout:  stderr: open /root/.bash_logout: permission denied
```

#### Solution
Choose one of the following:
  * Complete the build operation as a privileged user.
  * Install and configure fuse-overlayfs.
    * Install the fuse-overlayfs package for your Linux Distribution.
    * Add `mount_program = "/usr/bin/fuse-overlayfs"` under `[storage.options]` in your `~/.config/containers/storage.conf` file.
---
