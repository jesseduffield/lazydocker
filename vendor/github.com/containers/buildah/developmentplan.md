![buildah logo](https://cdn.rawgit.com/containers/buildah/main/logos/buildah-logo_large.png)

# Development Plan

## Development goals for Buildah

 *  Integration into Kubernetes and potentially other tools.  The biggest requirement for this is to be able run Buildah within a standard linux container without SYS_ADMIN privileges.  This would allow Buildah to run non-privileged containers inside of Kubernetes, so you could distribute your container workloads.

 * Integration with User Namespace, Podman has this already and the goal is to get `buildah build` and `buildah run` to be able to run its containers in a usernamespace to give the builder better security isolation from the host.

 * Buildah `buildah build` command's goal is to have feature parity with other OCI image and container build systems.

 * Addressing issues from the community as reported in the [Issues](https://github.com/containers/buildah/issues) page.
