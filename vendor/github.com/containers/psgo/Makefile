SHELL= /bin/bash
GO ?= go
BUILD_DIR := ./bin
BIN_DIR := /usr/local/bin
NAME := psgo
BATS_TESTS := *.bats

# Not all platforms support -buildmode=pie, plus it's incompatible with -race.
ifeq ($(shell $(GO) env GOOS),linux)
	ifeq (,$(filter $(shell $(GO) env GOARCH),mips mipsle mips64 mips64le ppc64 riscv64))
		ifeq (,$(findstring -race,$(EXTRA_BUILD_FLAGS)))
			GO_BUILDMODE := "-buildmode=pie"
		endif
	endif
endif
GO_BUILD := $(GO) build $(GO_BUILDMODE)

all: validate build

.PHONY: build
build:
	 $(GO_BUILD) $(EXTRA_BUILD_FLAGS) -o $(BUILD_DIR)/$(NAME) ./sample

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

.PHONY: vendor
vendor:
	go mod tidy
	go mod vendor
	go mod verify

.PHONY: validate
validate:
	golangci-lint run

.PHONY: test
test: test-unit test-integration

.PHONY: test-integration
test-integration:
	bats test/$(BATS_TESTS)

.PHONY: test-unit
test-unit:
	$(GO) test -v $(EXTRA_TEST_FLAGS) ./...

.PHONY: install
install:
	sudo install -D -m755 $(BUILD_DIR)/$(NAME) $(BIN_DIR)

.PHONY: uninstall
uninstall:
	sudo rm $(BIN_DIR)/$(NAME)
