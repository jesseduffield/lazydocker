ARG BASE_IMAGE_BUILDER=golang
ARG ALPINE_VERSION=3.20
ARG GO_VERSION=1.23

FROM ${BASE_IMAGE_BUILDER}:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder
ARG GOARCH=amd64
ARG GOARM
ARG VERSION
ARG VCS_REF
WORKDIR /tmp/gobuild
COPY ./ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} GOARM=${GOARM} \
    go build -a -mod=vendor \
    -ldflags="-s -w \
    -X main.commit=${VCS_REF} \
    -X main.version=${VERSION} \
    -X main.buildSource=Podman"

FROM ${BASE_IMAGE_BUILDER}:${GO_VERSION}-alpine${ALPINE_VERSION} AS podman-builder
ARG GOARCH=amd64
ARG GOARM
ARG PODMAN_VERSION=v5.3.1
RUN apk add -U -q --progress --no-cache curl tar gzip
WORKDIR /tmp/podman
# Download pre-built podman-remote binary from GitHub releases
RUN ARCH=$(case "${GOARCH}" in \
        amd64) echo "amd64" ;; \
        arm64) echo "arm64" ;; \
        arm) echo "arm" ;; \
        *) echo "amd64" ;; \
    esac) && \
    curl -fsSL "https://github.com/containers/podman/releases/download/${PODMAN_VERSION}/podman-remote-static-linux_${ARCH}.tar.gz" | \
    tar -xzf - && \
    mv bin/podman-remote-static-linux_${ARCH} /usr/local/bin/podman && \
    chmod +x /usr/local/bin/podman

FROM scratch
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION
LABEL \
    org.opencontainers.image.authors="christophe-duc" \
    org.opencontainers.image.created=$BUILD_DATE \
    org.opencontainers.image.version=$VERSION \
    org.opencontainers.image.revision=$VCS_REF \
    org.opencontainers.image.url="https://github.com/christophe-duc/lazypodman" \
    org.opencontainers.image.documentation="https://github.com/christophe-duc/lazypodman" \
    org.opencontainers.image.source="https://github.com/christophe-duc/lazypodman" \
    org.opencontainers.image.title="lazypodman" \
    org.opencontainers.image.description="The lazier way to manage everything podman"
ENTRYPOINT [ "/bin/lazypodman" ]
COPY --from=podman-builder /usr/local/bin/podman /bin/podman
COPY --from=builder /tmp/gobuild/lazypodman /bin/lazypodman
