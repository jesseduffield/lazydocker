package signal

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

// Make sure the signal buffer is sufficiently big.
// runc is using the same value.
const SignalBufferSize = 2048

// ParseSignal translates a string to a valid syscall signal.
// It returns an error if the signal map doesn't include the given signal.
func ParseSignal(rawSignal string) (syscall.Signal, error) {
	s, err := strconv.Atoi(rawSignal)
	if err == nil {
		if s == 0 {
			return -1, fmt.Errorf("invalid signal: %s", rawSignal)
		}
		return syscall.Signal(s), nil
	}
	sig, ok := SignalMap[strings.TrimPrefix(strings.ToUpper(rawSignal), "SIG")]
	if !ok {
		return -1, fmt.Errorf("invalid signal: %s", rawSignal)
	}
	return sig, nil
}

// ParseSignalNameOrNumber translates a string to a valid syscall signal.  Input
// can be a name or number representation i.e. "KILL" "9".
func ParseSignalNameOrNumber(rawSignal string) (syscall.Signal, error) {
	basename := strings.TrimPrefix(rawSignal, "-")
	s, err := ParseSignal(basename)
	if err == nil {
		return s, nil
	}
	for k, v := range SignalMap {
		if k == strings.ToUpper(basename) {
			return v, nil
		}
	}
	return -1, fmt.Errorf("invalid signal: %s", basename)
}

// CatchAll catches all signals (except the ones that make no sense to handle/forward,
// see isSignalIgnoredBySigProxy()) and relays them to the specified channel.
func CatchAll(sigc chan os.Signal) {
	handledSigs := make([]os.Signal, 0, len(SignalMap))
	for _, s := range SignalMap {
		if !isSignalIgnoredBySigProxy(s) {
			handledSigs = append(handledSigs, s)
		}
	}
	signal.Notify(sigc, handledSigs...)
}

// StopCatch stops catching the signals and closes the specified channel.
func StopCatch(sigc chan os.Signal) {
	signal.Stop(sigc)
	close(sigc)
}

// ParseSysSignalToName translates syscall.Signal to its name in the operating system.
// For example, syscall.Signal(9) will return "KILL" on Linux system.
func ParseSysSignalToName(s syscall.Signal) (string, error) {
	for k, v := range SignalMap {
		if v == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown syscall signal: %s", s)
}

func ToDockerFormat(s uint) string {
	var signalStr, err = ParseSysSignalToName(syscall.Signal(s))
	if err != nil {
		return strconv.FormatUint(uint64(s), 10)
	}
	return fmt.Sprintf("SIG%s", signalStr)
}
