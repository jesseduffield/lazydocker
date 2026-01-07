.PHONY: \
	all \
	binary \
	clean \
	containers-storage \
	cross \
	default \
	docs \
	gccgo \
	help \
	install \
	install.docs \
	lint \
	local-binary \
	local-cross \
	local-gccgo \
	local-test \
	local-test-integration \
	local-test-unit \
	test-integration \
	test-unit

NATIVETAGS :=
AUTOTAGS := $(shell ./hack/btrfs_tag.sh) $(shell ./hack/libsubid_tag.sh)
BUILDFLAGS := -tags "$(AUTOTAGS) $(TAGS)" $(FLAGS)
GO ?= go
TESTFLAGS := $(shell $(GO) test -race $(BUILDFLAGS) ./pkg/stringutils 2>&1 > /dev/null && echo -race)

default all: local-binary docs local-cross ## build and cross-build\nbinaries and docs

clean: ## remove all built files
	$(RM) -f containers-storage containers-storage.* docs/*.1 docs/*.5

containers-storage: ## build using gc on the host
	$(GO) build -compiler gc $(BUILDFLAGS) ./cmd/containers-storage

binary local-binary: containers-storage

local-gccgo gccgo: ## build using gccgo on the host
	GCCGO=$(PWD)/hack/gccgo-wrapper.sh $(GO) build -compiler gccgo $(BUILDFLAGS) -o containers-storage.gccgo ./cmd/containers-storage

local-cross cross: ## cross build the binaries for arm, darwin, and freebsd
	@for target in linux/amd64 linux/386 linux/arm linux/arm64 linux/ppc64 linux/ppc64le linux/riscv64 linux/s390x linux/mips linux/mipsle linux/mips64 linux/mips64le darwin/amd64 windows/amd64 freebsd/amd64 freebsd/arm64 ; do \
		os=`echo $${target} | cut -f1 -d/` ; \
		arch=`echo $${target} | cut -f2 -d/` ; \
		suffix=$${os}.$${arch} ; \
		echo env CGO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} $(GO) build -compiler gc -tags \"$(NATIVETAGS) $(TAGS)\" $(FLAGS) ./... ; \
		env CGO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} $(GO) build -compiler gc -tags "$(NATIVETAGS) $(TAGS)" $(FLAGS) ./... || exit 1 ; \
		echo env CGO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} $(GO) build -compiler gc -tags \"$(NATIVETAGS) $(TAGS)\" $(FLAGS) -o containers-storage.$${suffix} ./cmd/containers-storage ; \
		env CGO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} $(GO) build -compiler gc -tags "$(NATIVETAGS) $(TAGS)" $(FLAGS) -o containers-storage.$${suffix} ./cmd/containers-storage || exit 1 ; \
	done

docs: ## build the docs on the host
	$(MAKE) -C docs docs

local-test: local-binary local-test-unit local-test-integration ## build the binaries and run the tests

local-test-unit test-unit: local-binary ## run the unit tests on the host (requires\nsuperuser privileges)
	@$(GO) test -count 1 $(BUILDFLAGS) $(TESTFLAGS) ./...

local-test-integration test-integration: local-binary ## run the integration tests on the host (requires\nsuperuser privileges)
	@cd tests; ./test_runner.bash

install.docs: docs
	$(MAKE) -C docs install

install: install.docs

lint:
	golangci-lint run --build-tags="$(AUTOTAGS) $(TAGS)"

help: ## this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-z A-Z_-]+:.*?## / {gsub(" ",",",$$1);gsub("\\\\n",sprintf("\n%22c"," "), $$2);printf "\033[36m%-21s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: tidy-in-container
tidy-in-container:
	podman run --privileged --rm --env HOME=/root -v `pwd`:/src -w /src golang make tidy

.PHONY: tidy
tidy:
	$(GO) mod tidy
	$(GO) mod verify
	$(GO) mod edit -toolchain none
