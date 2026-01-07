//go:build !remote

package libpod

import (
	"errors"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/specgenutil"
	"go.podman.io/image/v5/manifest"
)

type HealthCheckConfig struct {
	*manifest.Schema2HealthConfig
}

type StartupHealthCheckConfig struct {
	*define.StartupHealthCheck
}

type IHealthCheckConfig interface {
	SetTo(config *ContainerConfig)
	IsStartup() bool
	IsNil() bool
	IsTimeChanged(oldInterval time.Duration) bool
	GetInterval() time.Duration
	SetCurrentConfigTo(healthCheckOptions *define.HealthCheckOptions)
	IsHealthCheckCommandSet(updateHealthCheckConfig define.UpdateHealthCheckConfig) bool
	SetNewHealthCheckOptions(updateHealthCheckConfig define.UpdateHealthCheckConfig, healthCheckOptions *define.HealthCheckOptions) bool
}

func (h *HealthCheckConfig) SetTo(config *ContainerConfig) {
	config.HealthCheckConfig = h.Schema2HealthConfig
}

func (h *StartupHealthCheckConfig) SetTo(config *ContainerConfig) {
	config.StartupHealthCheckConfig = h.StartupHealthCheck
}

func (h *HealthCheckConfig) IsNil() bool {
	return h.Schema2HealthConfig == nil
}

func (h *StartupHealthCheckConfig) IsNil() bool {
	return h.StartupHealthCheck == nil
}

func (h *HealthCheckConfig) IsStartup() bool {
	return false
}

func (h *StartupHealthCheckConfig) IsStartup() bool {
	return true
}

func (h *HealthCheckConfig) IsTimeChanged(oldInterval time.Duration) bool {
	return h.Interval != oldInterval
}

func (h *StartupHealthCheckConfig) IsTimeChanged(oldInterval time.Duration) bool {
	return h.Interval != oldInterval
}

func (h *HealthCheckConfig) GetInterval() time.Duration {
	return h.Interval
}

func (h *StartupHealthCheckConfig) GetInterval() time.Duration {
	return h.Interval
}

func (h *HealthCheckConfig) SetCurrentConfigTo(healthCheckOptions *define.HealthCheckOptions) {
	healthCheckOptions.Cmd = strings.Join(h.Test, " ")
	healthCheckOptions.Interval = h.Interval.String()
	healthCheckOptions.Retries = h.Retries
	healthCheckOptions.Timeout = h.Timeout.String()
	healthCheckOptions.StartPeriod = h.StartPeriod.String()
}

func (h *StartupHealthCheckConfig) SetCurrentConfigTo(healthCheckOptions *define.HealthCheckOptions) {
	healthCheckOptions.Cmd = strings.Join(h.Test, " ")
	healthCheckOptions.Interval = h.Interval.String()
	healthCheckOptions.Retries = h.Retries
	healthCheckOptions.Timeout = h.Timeout.String()
	healthCheckOptions.Successes = h.Successes
}

func (h *HealthCheckConfig) IsHealthCheckCommandSet(updateHealthCheckConfig define.UpdateHealthCheckConfig) bool {
	return updateHealthCheckConfig.IsHealthCheckCommandSet(h.Schema2HealthConfig)
}

func (h *StartupHealthCheckConfig) IsHealthCheckCommandSet(updateHealthCheckConfig define.UpdateHealthCheckConfig) bool {
	return updateHealthCheckConfig.IsStartupHealthCheckCommandSet(h.StartupHealthCheck)
}

func (h *HealthCheckConfig) SetNewHealthCheckOptions(updateHealthCheckConfig define.UpdateHealthCheckConfig, healthCheckOptions *define.HealthCheckOptions) bool {
	return updateHealthCheckConfig.SetNewHealthCheckConfigTo(healthCheckOptions)
}

func (h *StartupHealthCheckConfig) SetNewHealthCheckOptions(updateHealthCheckConfig define.UpdateHealthCheckConfig, healthCheckOptions *define.HealthCheckOptions) bool {
	return updateHealthCheckConfig.SetNewStartupHealthCheckConfigTo(healthCheckOptions)
}

func GetNewHealthCheckConfig(originalHealthCheckConfig IHealthCheckConfig, updateHealthCheckConfig define.UpdateHealthCheckConfig) (IHealthCheckConfig, bool, error) {
	if originalHealthCheckConfig.IsHealthCheckCommandSet(updateHealthCheckConfig) {
		return nil, false, errors.New("startup healthcheck command is not set")
	}

	healthCheckOptions := define.HealthCheckOptions{
		Cmd:         "",
		Interval:    define.DefaultHealthCheckInterval,
		Retries:     int(define.DefaultHealthCheckRetries),
		Timeout:     define.DefaultHealthCheckTimeout,
		StartPeriod: define.DefaultHealthCheckStartPeriod,
		Successes:   0,
	}

	if originalHealthCheckConfig.IsStartup() {
		healthCheckOptions.Retries = 0
	}

	if !originalHealthCheckConfig.IsNil() {
		originalHealthCheckConfig.SetCurrentConfigTo(&healthCheckOptions)
	}

	noHealthCheck := false
	if updateHealthCheckConfig.NoHealthCheck != nil {
		noHealthCheck = *updateHealthCheckConfig.NoHealthCheck
	}

	changed := originalHealthCheckConfig.SetNewHealthCheckOptions(updateHealthCheckConfig, &healthCheckOptions)

	if noHealthCheck && changed {
		return nil, false, errors.New("cannot specify both --no-healthcheck and other HealthCheck flags")
	}

	if noHealthCheck {
		if originalHealthCheckConfig.IsStartup() {
			return &StartupHealthCheckConfig{StartupHealthCheck: nil}, true, nil
		}
		return &HealthCheckConfig{Schema2HealthConfig: &manifest.Schema2HealthConfig{Test: []string{"NONE"}}}, true, nil
	}

	newHealthCheckConfig, err := specgenutil.MakeHealthCheckFromCli(
		healthCheckOptions.Cmd,
		healthCheckOptions.Interval,
		uint(healthCheckOptions.Retries),
		healthCheckOptions.Timeout,
		healthCheckOptions.StartPeriod,
		true,
	)
	if err != nil {
		return nil, false, err
	}

	if originalHealthCheckConfig.IsStartup() {
		newStartupHealthCheckConfig := new(define.StartupHealthCheck)
		newStartupHealthCheckConfig.Schema2HealthConfig = *newHealthCheckConfig
		newStartupHealthCheckConfig.Successes = healthCheckOptions.Successes
		return &StartupHealthCheckConfig{StartupHealthCheck: newStartupHealthCheckConfig}, changed, nil
	}
	return &HealthCheckConfig{Schema2HealthConfig: newHealthCheckConfig}, changed, nil
}
