.PHONY: test
test:
	go test -v -race ./...

.PHONY: simple-test
simple-test:
	go test -v ./...

.PHONY: cover
cover:
	go test -coverprofile=cover.out ./...

.PHONY: cover-html
cover-html: cover
	go tool cover -html=cover.out

.PHONY: ycat/build
ycat/build:
	go build -o ycat ./cmd/ycat
