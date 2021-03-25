FROM golang:1.13-alpine3.10
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=1000
RUN adduser $USERNAME -s /bin/sh -D -u $USER_UID $USER_GID && \
    mkdir -p /etc/sudoers.d && \
    echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME && \
    chmod 0440 /etc/sudoers.d/$USERNAME
RUN apk add -q --update --progress --no-cache git sudo
RUN go get -u -v golang.org/x/tools/cmd/gopls 2>&1