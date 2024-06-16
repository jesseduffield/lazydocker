ARG BASE_IMAGE_BUILDER=golang
ARG ALPINE_VERSION=3.15
ARG GO_VERSION=1.20

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
    -X main.buildSource=Docker"

FROM ${BASE_IMAGE_BUILDER}:${GO_VERSION}-alpine${ALPINE_VERSION} AS docker-builder
ARG GOARCH=amd64
ARG GOARM
ARG DOCKER_VERSION=v20.10.13
RUN apk add -U -q --progress --no-cache git bash coreutils gcc musl-dev
WORKDIR /go/src/github.com/docker/cli
RUN git clone --branch ${DOCKER_VERSION} --single-branch --depth 1 https://github.com/docker/cli.git . > /dev/null 2>&1
ENV CGO_ENABLED=0 \
    GOARCH=${GOARCH} \
    GOARM=${GOARM} \
    DISABLE_WARN_OUTSIDE_CONTAINER=1
RUN ./scripts/build/binary
RUN rm build/docker && mv build/docker-linux-* build/docker

FROM scratch
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION
LABEL \
    org.opencontainers.image.authors="jessedduffield@gmail.com" \
    org.opencontainers.image.created=$BUILD_DATE \
    org.opencontainers.image.version=$VERSION \
    org.opencontainers.image.revision=$VCS_REF \
    org.opencontainers.image.url="https://github.com/jesseduffield/lazydocker" \
    org.opencontainers.image.documentation="https://github.com/jesseduffield/lazydocker" \
    org.opencontainers.image.source="https://github.com/jesseduffield/lazydocker" \
    org.opencontainers.image.title="lazydocker" \
    org.opencontainers.image.description="The lazier way to manage everything docker"
ENTRYPOINT [ "/bin/lazydocker" ]
COPY --from=docker-builder /go/src/github.com/docker/cli/build/docker /bin/docker
COPY --from=builder /tmp/gobuild/lazydocker /bin/lazydocker
