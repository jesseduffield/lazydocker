BUMP_VERSION := $(GOPATH)/bin/bump_version
WRITE_MAILMAP := $(GOPATH)/bin/write_mailmap

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

test:
	@# the timeout helps guard against infinite recursion
	go test -timeout=250ms ./...

race-test:
	go test -timeout=500ms -race ./...

$(BUMP_VERSION):
	go get -u github.com/kevinburke/bump_version

$(WRITE_MAILMAP):
	go get -u github.com/kevinburke/write_mailmap

release: test | $(BUMP_VERSION)
	$(BUMP_VERSION) --tag-prefix=v minor config.go

force: ;

AUTHORS.txt: force | $(WRITE_MAILMAP)
	$(WRITE_MAILMAP) > AUTHORS.txt

authors: AUTHORS.txt
