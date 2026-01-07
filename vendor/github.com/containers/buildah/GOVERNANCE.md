## The Buildah Project Community Governance

The Buildah project, as part of Podman Container Tools, follows the [Podman Project Governance](https://github.com/containers/podman/blob/main/GOVERNANCE.md)
except sections found in this document, which override those found in Podman's Governance.

---

# Maintainers File

The definitive source of truth for maintainers of this repository is the local [MAINTAINERS.md](./MAINTAINERS.md) file. The [MAINTAINERS.md](https://github.com/containers/podman/blob/main/MAINTAINERS.md) file in the main Podman repository is used for project-spanning roles, including Core Maintainer and Community Manager. Some repositories in the project will also have a local [OWNERS](./OWNERS) file, which the CI system uses to map users to roles. Any changes to the [OWNERS](./OWNERS) file must make a corresponding change to the [MAINTAINERS.md](./MAINTAINERS.md) file to ensure that the file remains up to date. Most changes to [MAINTAINERS.md](./MAINTAINERS.md) will require a change to the repository’s [OWNERS](.OWNERS) file (e.g., adding a Reviewer), but some will not (e.g., promoting a Maintainer to a Core Maintainer, which comes with no additional CI-related privileges).

Any Core Maintainers listed in Podman’s [MAINTAINERS.md](https://github.com/containers/podman/blob/main/MAINTAINERS.md) file should also be added to the list of “approvers” in the local [OWNERS](./OWNERS) file and as a Core Maintainer in the list of “Maintainers” in the local [MAINTAINERS.md](./MAINTAINERS.md) file.
