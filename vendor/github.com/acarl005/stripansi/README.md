Strip ANSI
==========

This Go package removes ANSI escape codes from strings.

Ideally, we would prevent these from appearing in any text we want to process.
However, sometimes this can't be helped, and we need to be able to deal with that noise.
This will use a regexp to remove those unwanted escape codes.


## Install

```sh
$ go get -u github.com/acarl005/stripansi
```

## Usage

```go
import (
	"fmt"
	"github.com/acarl005/stripansi"
)

func main() {
	msg := "\x1b[38;5;140m foo\x1b[0m bar"
	cleanMsg := stripansi.Strip(msg)
	fmt.Println(cleanMsg) // " foo bar"
}
```
