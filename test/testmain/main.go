package main

import "log"

func main() {
	c := make(chan struct{})

	close(c)

	close(c)

	select {
	case <-c:
		log.Println("hmm")
	}

	select {
	case <-c:
		log.Println("okay")
	}
}
