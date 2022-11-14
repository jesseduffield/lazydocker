FROM mcr.microsoft.com/devcontainers/go:bullseye

RUN apt-get update && apt-get install -y \
      curl \
      git \
    && rm -rf /var/lib/apt/lists/*

RUN go install mvdan.cc/gofumpt@latest
ENV PATH="/root/go/bin:${PATH}"
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.50.0
RUN golangci-lint --version
