ARG BASE_IMAGE_BUILDER=golang
ARG ALPINE_VERSION=3.10
ARG GO_VERSION=1.12.6

FROM ${BASE_IMAGE_BUILDER}:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder
ARG GOARCH=amd64
ARG GOARM
ARG VERSION
ARG VCS_REF
WORKDIR /go/src/github.com/jesseduffield/lazydocker/
COPY ./ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} GOARM=${GOARM} go build -a -ldflags="-s -w \
    -X main.commit=${VCS_REF} \
    -X main.version=${VERSION} \
    -X main.buildSource=Docker"

FROM ${BASE_IMAGE_BUILDER}:${GO_VERSION}-alpine${ALPINE_VERSION} AS docker-builder
ARG GOARCH=amd64
ARG GOARM
ARG DOCKER_VERSION=v18.09.7
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
LABEL org.label-schema.schema-version="1.0.0-rc1" \
    maintainer="jessedduffield@gmail.com" \
    org.label-schema.build-date=$BUILD_DATE \
    org.label-schema.vcs-ref=$VCS_REF \
    org.label-schema.url="https://github.com/jesseduffield/lazydocker" \
    org.label-schema.vcs-description="The lazier way to manage everything docker" \
    org.label-schema.docker.cmd="docker run -it -v /var/run/docker.sock:/var/run/docker.sock lazydocker" \
    org.label-schema.version=${VERSION}
ENTRYPOINT [ "/bin/lazydocker" ]
VOLUME [ "/.config/jesseduffield/lazydocker" ]
ENV PATH=/bin
COPY --from=builder /go/src/github.com/jesseduffield/lazydocker/lazydocker /bin/lazydocker
COPY --from=docker-builder /go/src/github.com/docker/cli/build/docker /bin/docker