ARG BASE_IMAGE_BUILDER=golang
ARG BASE_IMAGE=alpine
ARG ALPINE_VERSION=3.10
ARG GO_VERSION=1.12.6

FROM ${BASE_IMAGE_BUILDER}:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder
ARG GOARCH=amd64
ARG GOARM
WORKDIR /go/src/github.com/jesseduffield/lazydocker/
COPY ./ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} GOARM=${GOARM} go build -a -installsuffix cgo -ldflags="-s -w"

FROM ${BASE_IMAGE}:${ALPINE_VERSION}
RUN apk --update add -q --progress --no-cache -U git xdg-utils
ENTRYPOINT [ "lazydocker" ]
COPY --from=builder /go/src/github.com/jesseduffield/lazydocker/lazydocker /usr/bin/lazydocker
