//go:build !systemd || !cgo

package config

const (
	// DefaultLogDriver is the default type of log files
	DefaultLogDriver = "k8s-file"
)

func defaultCgroupManager() string {
	return CgroupfsCgroupsManager
}

func defaultEventsLogger() string {
	return "file"
}

func defaultLogDriver() string {
	return DefaultLogDriver
}

func useSystemd() bool {
	return false
}

func useJournald() bool {
	return false
}
