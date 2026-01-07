# go-dockerclient

[![Build Status](https://github.com/fsouza/go-dockerclient/workflows/Build/badge.svg)](https://github.com/fsouza/go-dockerclient/actions?query=branch:main+workflow:Build)
[![GoDoc](https://img.shields.io/badge/api-Godoc-blue.svg?style=flat-square)](https://pkg.go.dev/github.com/fsouza/go-dockerclient)

This package presents a client for the Docker remote API. It also provides
support for the extensions in the [Swarm API](https://docs.docker.com/swarm/swarm-api/).

This package also provides support for docker's network API, which is a simple
passthrough to the libnetwork remote API.

For more details, check the [remote API
documentation](https://docs.docker.com/engine/api/latest/).

## Difference between go-dockerclient and the official SDK

Link for the official SDK: https://docs.docker.com/develop/sdk/

go-dockerclient was created before Docker had an official Go SDK and is
still maintained and active because it's still used out there. New features in
the Docker API do not get automatically implemented here: it's based on demand,
if someone wants it, they can file an issue or a PR and the feature may get
implemented/merged.

For new projects, using the official SDK is probably more appropriate as
go-dockerclient lags behind the official SDK.

## Example

```go
package main

import (
	"fmt"

	docker "github.com/fsouza/go-dockerclient"
)

func main() {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		panic(err)
	}
	imgs, err := client.ListImages(docker.ListImagesOptions{All: false})
	if err != nil {
		panic(err)
	}
	for _, img := range imgs {
		fmt.Println("ID: ", img.ID)
		fmt.Println("RepoTags: ", img.RepoTags)
		fmt.Println("Created: ", img.Created)
		fmt.Println("Size: ", img.Size)
		fmt.Println("VirtualSize: ", img.VirtualSize)
		fmt.Println("ParentId: ", img.ParentID)
	}
}
```

## Using with TLS

In order to instantiate the client for a TLS-enabled daemon, you should use
NewTLSClient, passing the endpoint and path for key and certificates as
parameters.

```go
package main

import (
	"fmt"

	docker "github.com/fsouza/go-dockerclient"
)

func main() {
	const endpoint = "tcp://[ip]:[port]"
	path := os.Getenv("DOCKER_CERT_PATH")
	ca := fmt.Sprintf("%s/ca.pem", path)
	cert := fmt.Sprintf("%s/cert.pem", path)
	key := fmt.Sprintf("%s/key.pem", path)
	client, _ := docker.NewTLSClient(endpoint, cert, key, ca)
	// use client
}
```

If using [docker-machine](https://docs.docker.com/machine/), or another
application that exports environment variables `DOCKER_HOST`,
`DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH`, `DOCKER_API_VERSION`, you can use
NewClientFromEnv.


```go
package main

import (
	"fmt"

	docker "github.com/fsouza/go-dockerclient"
)

func main() {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		// handle err
	}
	// use client
}
```

See the documentation for more details.

## Developing

All development commands can be seen in the [Makefile](Makefile).

Committed code must pass:

* [golangci-lint](https://github.com/golangci/golangci-lint)
* [go test](https://golang.org/cmd/go/#hdr-Test_packages)
* [staticcheck](https://staticcheck.io/)

Running ``make test`` will run all checks, as well as install any required
dependencies.
