# OCI Hooks Configuration

For POSIX platforms, the [OCI runtime configuration][runtime-spec] supports [hooks][spec-hooks] for configuring custom actions related to the life cycle of the container.
The way you enable the hooks above is by editing the OCI runtime configuration before running the OCI runtime (e.g. [`runc`][runc]).
CRI-O and `podman create` create the OCI configuration for you, and this documentation allows developers to configure them to set their intended hooks.

One problem with hooks is that the runtime actually stalls execution of the container before running the hooks and stalls completion of the container, until all hooks complete.
This can cause some performance issues.
Also a lot of hooks just check if certain configuration is set and then exit early, without doing anything.
For example the [oci-systemd-hook][] only executes if the command is `init` or `systemd`, otherwise it just exits.
This means if we automatically enabled all hooks, every container would have to execute `oci-systemd-hook`, even if they don't run systemd inside of the container.
Performance would also suffer if we executed each hook at each stage ([pre-start][], [post-start][], and [post-stop][]).

The hooks configuration is documented in [`oci-hooks.5`](docs/oci-hooks.5.md).

[oci-systemd-hook]: https://github.com/projectatomic/oci-systemd-hook
[post-start]: https://github.com/opencontainers/runtime-spec/blob/v1.0.1/config.md#poststart
[post-stop]: https://github.com/opencontainers/runtime-spec/blob/v1.0.1/config.md#poststop
[pre-start]: https://github.com/opencontainers/runtime-spec/blob/v1.0.1/config.md#prestart
[runc]: https://github.com/opencontainers/runc
[runtime-spec]: https://github.com/opencontainers/runtime-spec/blob/v1.0.1/spec.md
[spec-hooks]: https://github.com/opencontainers/runtime-spec/blob/v1.0.1/config.md#posix-platform-hooks
