# Multi Progress Bar

[![GoDoc](https://pkg.go.dev/badge/github.com/vbauerster/mpb)](https://pkg.go.dev/github.com/vbauerster/mpb/v8)
[![Test status](https://github.com/vbauerster/mpb/actions/workflows/test.yml/badge.svg)](https://github.com/vbauerster/mpb/actions/workflows/test.yml)
[![Lint status](https://github.com/vbauerster/mpb/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/vbauerster/mpb/actions/workflows/golangci-lint.yml)

**mpb** is a Go lib for rendering progress bars in terminal applications.

## Features

- **Multiple Bars**: Multiple progress bars are supported
- **Dynamic Total**: Set total while bar is running
- **Dynamic Add/Remove**: Dynamically add or remove bars
- **Cancellation**: Cancel whole rendering process
- **Predefined Decorators**: Elapsed time, [ewma](https://github.com/VividCortex/ewma) based ETA, Percentage, Bytes counter
- **Decorator's width sync**: Synchronized decorator's width among multiple bars

## Usage

#### [Rendering single bar](_examples/singleBar/main.go)

```go
package main

import (
    "math/rand"
    "time"

    "github.com/vbauerster/mpb/v8"
    "github.com/vbauerster/mpb/v8/decor"
)

func main() {
    // initialize progress container, with custom width
    p := mpb.New(mpb.WithWidth(64))

    total := 100
    name := "Single Bar:"
    // create a single bar, which will inherit container's width
    bar := p.New(int64(total),
        // BarFillerBuilder with custom style
        mpb.BarStyle().Lbound("╢").Filler("▌").Tip("▌").Padding("░").Rbound("╟"),
        mpb.PrependDecorators(
            // display our name with one space on the right
            decor.Name(name, decor.WC{C: decor.DindentRight | decor.DextraSpace}),
            // replace ETA decorator with "done" message, OnComplete event
            decor.OnComplete(decor.AverageETA(decor.ET_STYLE_GO), "done"),
        ),
        mpb.AppendDecorators(decor.Percentage()),
    )
    // simulating some work
    max := 100 * time.Millisecond
    for i := 0; i < total; i++ {
        time.Sleep(time.Duration(rand.Intn(10)+1) * max / 10)
        bar.Increment()
    }
    // wait for our bar to complete and flush
    p.Wait()
}
```

#### [Rendering multiple bars](_examples/multiBars/main.go)

```go
    var wg sync.WaitGroup
    // passed wg will be accounted at p.Wait() call
    p := mpb.New(mpb.WithWaitGroup(&wg))
    total, numBars := 100, 3
    wg.Add(numBars)

    for i := 0; i < numBars; i++ {
        name := fmt.Sprintf("Bar#%d:", i)
        bar := p.AddBar(int64(total),
            mpb.PrependDecorators(
                // simple name decorator
                decor.Name(name),
                // decor.DSyncWidth bit enables column width synchronization
                decor.Percentage(decor.WCSyncSpace),
            ),
            mpb.AppendDecorators(
                // replace ETA decorator with "done" message, OnComplete event
                decor.OnComplete(
                    // ETA decorator with ewma age of 30
                    decor.EwmaETA(decor.ET_STYLE_GO, 30, decor.WCSyncWidth), "done",
                ),
            ),
        )
        // simulating some work
        go func() {
            defer wg.Done()
            rng := rand.New(rand.NewSource(time.Now().UnixNano()))
            max := 100 * time.Millisecond
            for i := 0; i < total; i++ {
                // start variable is solely for EWMA calculation
                // EWMA's unit of measure is an iteration's duration
                start := time.Now()
                time.Sleep(time.Duration(rng.Intn(10)+1) * max / 10)
                // we need to call EwmaIncrement to fulfill ewma decorator's contract
                bar.EwmaIncrement(time.Since(start))
            }
        }()
    }
    // wait for passed wg and for all bars to complete and flush
    p.Wait()
```

#### [dynTotal example](_examples/dynTotal/main.go)

![dynTotal](_svg/godEMrCZmJkHYH1X9dN4Nm0U7.svg)

#### [complex example](_examples/complex/main.go)

![complex](_svg/wHzf1M7sd7B3zVa2scBMnjqRf.svg)

#### [io example](_examples/io/main.go)

![io](_svg/hIpTa3A5rQz65ssiVuRJu87X6.svg)
