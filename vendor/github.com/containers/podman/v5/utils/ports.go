package utils

import (
	"fmt"
	"net"
	"strconv"
)

// Find a random, open port on the host.
func GetRandomPort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, fmt.Errorf("unable to get free TCP port: %w", err)
	}
	defer l.Close()
	_, randomPort, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		return 0, fmt.Errorf("unable to determine free port: %w", err)
	}
	rp, err := strconv.Atoi(randomPort)
	if err != nil {
		return 0, fmt.Errorf("unable to convert random port to int: %w", err)
	}
	return rp, nil
}
