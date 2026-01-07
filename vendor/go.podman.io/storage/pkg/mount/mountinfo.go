package mount

import (
	"github.com/moby/sys/mountinfo"
)

type Info = mountinfo.Info

var Mounted = mountinfo.Mounted

func GetMounts() ([]*Info, error) {
	return mountinfo.GetMounts(nil)
}
