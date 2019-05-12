package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"time"
)

func main() {
	exitOnInterrupt()

	for range time.Tick(time.Second / 3) {
		fmt.Println(rand.Intn(1000))
	}
}

func exitOnInterrupt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		os.Exit(0)
	}()
}
