build:
	go build ./cmd/imagebuilder
.PHONY: build

test:
	go test ./...
.PHONY: test

test-conformance:
	go test -v -tags conformance -timeout 45m ./dockerclient
.PHONY: test-conformance

.PHONY: vendor
vendor:
	GO111MODULE=on go mod tidy
	GO111MODULE=on go mod vendor
	GO111MODULE=on go mod verify
