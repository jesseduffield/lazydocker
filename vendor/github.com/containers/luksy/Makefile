GO = go
BATS = bats

all: luksy

luksy: cmd/luksy/*.go *.go
	$(GO) build -o luksy$(shell go env GOEXE) ./cmd/luksy

clean:
	$(RM) luksy$(shell go env GOEXE) luksy.test

test:
	$(GO) test -timeout 45m -v -cover
	$(BATS) ./tests
