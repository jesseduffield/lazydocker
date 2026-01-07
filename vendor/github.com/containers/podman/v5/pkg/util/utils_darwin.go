//go:build darwin

package util

import (
	"errors"
)

func GetContainerPidInformationDescriptors() ([]string, error) {
	return []string{}, errors.New("this function is not supported on darwin")
}
