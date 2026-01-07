// In go 1.20 and later, the global RNG is automatically initialized.
// Ref: https://pkg.go.dev/math/rand@go1.20#Seed
//go:build !go1.20 && !remote

package libpod

import (
	"math/rand"
	"time"
)

func init() {
	// generateName calls namesgenerator.GetRandomName which the
	// global RNG from math/rand. Seed it here to make sure we
	// don't get the same name every time.
	rand.Seed(time.Now().UnixNano())
}
