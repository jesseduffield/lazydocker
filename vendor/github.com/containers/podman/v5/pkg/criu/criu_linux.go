//go:build linux

package criu

import (
	"fmt"

	"github.com/checkpoint-restore/go-criu/v7"
	"github.com/checkpoint-restore/go-criu/v7/rpc"

	"google.golang.org/protobuf/proto"
)

// CheckForCriu uses CRIU's go bindings to check if the CRIU
// binary exists and if it at least the version Podman needs.
func CheckForCriu(version int) error {
	c := criu.MakeCriu()
	criuVersion, err := c.GetCriuVersion()
	if err != nil {
		return fmt.Errorf("failed to check for criu version: %w", err)
	}

	if criuVersion >= version {
		return nil
	}
	return fmt.Errorf("checkpoint/restore requires at least CRIU %d, current version is %d", version, criuVersion)
}

func MemTrack() bool {
	features, err := criu.MakeCriu().FeatureCheck(
		&rpc.CriuFeatures{
			MemTrack: proto.Bool(true),
		},
	)
	if err != nil {
		return false
	}

	return features.GetMemTrack()
}

func GetCriuVersion() (int, error) {
	c := criu.MakeCriu()
	return c.GetCriuVersion()
}
