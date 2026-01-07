//go:build freebsd
// +build freebsd

package jail

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/containers/buildah/pkg/util"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

type NS int32

const (
	DISABLED NS = 0
	NEW      NS = 1
	INHERIT  NS = 2

	JAIL_CREATE = 0x01
	JAIL_UPDATE = 0x02
	JAIL_ATTACH = 0x04
)

type config struct {
	params map[string]any
}

var (
	needVnetJailOnce sync.Once
	needVnetJail     bool
)

func NewConfig() *config {
	return &config{
		params: make(map[string]any),
	}
}

func handleBoolSetting(key string, val bool) (string, any) {
	// jail doesn't deal with booleans - it uses paired parameter
	// names, e.g. "persist"/"nopersist". If the key contains '.',
	// the "no" prefix is applied to the last element.
	if val == false {
		parts := strings.Split(key, ".")
		parts[len(parts)-1] = "no" + parts[len(parts)-1]
		key = strings.Join(parts, ".")
	}
	return key, nil
}

func (c *config) Set(key string, value any) {
	// Normalise integer types to int32
	switch v := value.(type) {
	case int:
		value = int32(v)
	case uint32:
		value = int32(v)
	}

	switch key {
	case "jid", "devfs_ruleset", "enforce_statfs", "children.max", "securelevel":
		if _, ok := value.(int32); !ok {
			logrus.Fatalf("value for parameter %s must be an int32", key)
		}
	case "ip4", "ip6", "host", "vnet":
		nsval, ok := value.(NS)
		if !ok {
			logrus.Fatalf("value for parameter %s must be a jail.NS", key)
		}
		if (key == "host" || key == "vnet") && nsval == DISABLED {
			logrus.Fatalf("value for parameter %s cannot be DISABLED", key)
		}
	case "persist", "sysvmsg", "sysvsem", "sysvshm":
		bval, ok := value.(bool)
		if !ok {
			logrus.Fatalf("value for parameter %s must be bool", key)
		}
		key, value = handleBoolSetting(key, bval)
	default:
		if strings.HasPrefix(key, "allow.") {
			bval, ok := value.(bool)
			if !ok {
				logrus.Fatalf("value for parameter %s must be bool", key)
			}
			key, value = handleBoolSetting(key, bval)
		} else {
			if _, ok := value.(string); !ok {
				logrus.Fatalf("value for parameter %s must be a string", key)
			}
		}
	}
	c.params[key] = value
}

func (c *config) getIovec() ([]syscall.Iovec, error) {
	jiov := make([]syscall.Iovec, 0)
	for key, value := range c.params {
		iov, err := stringToIovec(key)
		if err != nil {
			return nil, err
		}
		jiov = append(jiov, iov)
		switch v := value.(type) {
		case string:
			iov, err := stringToIovec(v)
			if err != nil {
				return nil, err
			}
			jiov = append(jiov, iov)
		case int32:
			jiov = append(jiov, syscall.Iovec{
				Base: (*byte)(unsafe.Pointer(&v)),
				Len:  4,
			})
		case NS:
			jiov = append(jiov, syscall.Iovec{
				Base: (*byte)(unsafe.Pointer(&v)),
				Len:  4,
			})
		default:
			jiov = append(jiov, syscall.Iovec{
				Base: nil,
				Len:  0,
			})
		}
	}
	return jiov, nil
}

type jail struct {
	jid int32
}

func jailSet(jconf *config, flags int) (*jail, error) {
	jiov, err := jconf.getIovec()
	if err != nil {
		return nil, err
	}

	jid, _, errno := syscall.Syscall(unix.SYS_JAIL_SET, uintptr(unsafe.Pointer(&jiov[0])), uintptr(len(jiov)), uintptr(flags))
	if errno != 0 {
		return nil, errno
	}
	return &jail{
		jid: int32(jid),
	}, nil
}

func jailGet(jconf *config, flags int) (*jail, error) {
	jiov, err := jconf.getIovec()
	if err != nil {
		return nil, err
	}

	jid, _, errno := syscall.Syscall(unix.SYS_JAIL_GET, uintptr(unsafe.Pointer(&jiov[0])), uintptr(len(jiov)), uintptr(flags))
	if errno != 0 {
		return nil, errno
	}
	return &jail{
		jid: int32(jid),
	}, nil
}

func Create(jconf *config) (*jail, error) {
	return jailSet(jconf, JAIL_CREATE)
}

func CreateAndAttach(jconf *config) (*jail, error) {
	return jailSet(jconf, JAIL_CREATE|JAIL_ATTACH)
}

func FindByName(name string) (*jail, error) {
	jconf := NewConfig()
	jconf.Set("name", name)
	return jailGet(jconf, 0)
}

func (j *jail) Set(jconf *config) error {
	jconf.Set("jid", j.jid)
	_, err := jailSet(jconf, JAIL_UPDATE)
	return err
}

func parseVersion(version string) (string, int, int, int, error) {
	// Expected formats:
	//	<major>.<minor>-RELEASE optionally followed by -p<patchlevel>
	//	<major>-STABLE
	//	<major>-CURRENT
	parts := strings.Split(string(version), "-")
	if len(parts) < 2 || len(parts) > 3 {
		return "", -1, -1, -1, fmt.Errorf("unexpected OS version: %s", version)
	}
	ver := strings.Split(parts[0], ".")

	if len(ver) != 2 {
		return "", -1, -1, -1, fmt.Errorf("unexpected OS version: %s", version)
	}
	major, err := strconv.Atoi(ver[0])
	if err != nil {
		return "", -1, -1, -1, fmt.Errorf("unexpected OS version: %s", version)
	}
	minor, err := strconv.Atoi(ver[1])
	if err != nil {
		return "", -1, -1, -1, fmt.Errorf("unexpected OS version: %s", version)
	}
	patchlevel := 0
	if len(parts) == 3 {
		if parts[1] != "RELEASE" || !strings.HasPrefix(parts[2], "p") {
			return "", -1, -1, -1, fmt.Errorf("unexpected OS version: %s", version)
		}
		patchlevel, err = strconv.Atoi(strings.TrimPrefix(parts[2], "p"))
		if err != nil {
			return "", -1, -1, -1, fmt.Errorf("unexpected OS version: %s", version)
		}
	}
	return parts[1], major, minor, patchlevel, nil
}

// Return true if its necessary to have a separate jail to own the vnet.  For
// FreeBSD 13.3 and later, we don't need a separate vnet jail since it is
// possible to configure the network without either attaching to the container's
// jail or trusting the ifconfig and route utilities in the container. If for
// any reason, we fail to parse the OS version, we default to returning true.
func NeedVnetJail() bool {
	needVnetJailOnce.Do(func() {
		// FreeBSD 13.3 and later have support for 'ifconfig -j' and 'route -j'
		needVnetJail = true
		version, err := util.ReadKernelVersion()
		if err != nil {
			logrus.Errorf("failed to determine OS version: %v", err)
			return
		}
		_, major, minor, _, err := parseVersion(version)
		if major > 13 || (major == 13 && minor > 2) {
			needVnetJail = false
		}
	})
	return needVnetJail
}
