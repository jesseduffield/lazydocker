//go:build freebsd

package archive

import (
	"archive/tar"
	"fmt"
	"math/bits"
	"os"
	"strings"
	"syscall"

	"go.podman.io/storage/pkg/system"
)

const (
	paxSCHILYFflags = "SCHILY.fflags"
)

var (
	flagNameToValue = map[string]uint32{
		"sappnd":     system.SF_APPEND,
		"sappend":    system.SF_APPEND,
		"arch":       system.SF_ARCHIVED,
		"archived":   system.SF_ARCHIVED,
		"schg":       system.SF_IMMUTABLE,
		"schange":    system.SF_IMMUTABLE,
		"simmutable": system.SF_IMMUTABLE,
		"sunlnk":     system.SF_NOUNLINK,
		"sunlink":    system.SF_NOUNLINK,
		"snapshot":   system.SF_SNAPSHOT,
		"uappnd":     system.UF_APPEND,
		"uappend":    system.UF_APPEND,
		"uarch":      system.UF_ARCHIVE,
		"uarchive":   system.UF_ARCHIVE,
		"hidden":     system.UF_HIDDEN,
		"uhidden":    system.UF_HIDDEN,
		"uchg":       system.UF_IMMUTABLE,
		"uchange":    system.UF_IMMUTABLE,
		"uimmutable": system.UF_IMMUTABLE,
		"uunlnk":     system.UF_NOUNLINK,
		"uunlink":    system.UF_NOUNLINK,
		"offline":    system.UF_OFFLINE,
		"uoffline":   system.UF_OFFLINE,
		"opaque":     system.UF_OPAQUE,
		"rdonly":     system.UF_READONLY,
		"urdonly":    system.UF_READONLY,
		"readonly":   system.UF_READONLY,
		"ureadonly":  system.UF_READONLY,
		"reparse":    system.UF_REPARSE,
		"ureparse":   system.UF_REPARSE,
		"sparse":     system.UF_SPARSE,
		"usparse":    system.UF_SPARSE,
		"system":     system.UF_SYSTEM,
		"usystem":    system.UF_SYSTEM,
	}
	// Only include the short names for the reverse map
	flagValueToName = map[uint32]string{
		system.SF_APPEND:    "sappnd",
		system.SF_ARCHIVED:  "arch",
		system.SF_IMMUTABLE: "schg",
		system.SF_NOUNLINK:  "sunlnk",
		system.SF_SNAPSHOT:  "snapshot",
		system.UF_APPEND:    "uappnd",
		system.UF_ARCHIVE:   "uarch",
		system.UF_HIDDEN:    "hidden",
		system.UF_IMMUTABLE: "uchg",
		system.UF_NOUNLINK:  "uunlnk",
		system.UF_OFFLINE:   "offline",
		system.UF_OPAQUE:    "opaque",
		system.UF_READONLY:  "rdonly",
		system.UF_REPARSE:   "reparse",
		system.UF_SPARSE:    "sparse",
		system.UF_SYSTEM:    "system",
	}
)

func parseFileFlags(fflags string) (uint32, uint32, error) {
	var set, clear uint32 = 0, 0
	for fflag := range strings.SplitSeq(fflags, ",") {
		isClear := false
		if clean, ok := strings.CutPrefix(fflag, "no"); ok {
			isClear = true
			fflag = clean
		}
		if value, ok := flagNameToValue[fflag]; ok {
			if isClear {
				clear |= value
			} else {
				set |= value
			}
		} else {
			return 0, 0, fmt.Errorf("parsing file flags, unrecognised token: %s", fflag)
		}
	}
	return set, clear, nil
}

func formatFileFlags(fflags uint32) (string, error) {
	res := []string{}
	for fflags != 0 {
		// Extract lowest set bit
		fflag := uint32(1) << bits.TrailingZeros32(fflags)
		if name, ok := flagValueToName[fflag]; ok {
			res = append(res, name)
		} else {
			return "", fmt.Errorf("formatting file flags, unrecognised flag: %x", fflag)
		}
		fflags &= ^fflag
	}
	return strings.Join(res, ","), nil
}

func ReadFileFlagsToTarHeader(path string, hdr *tar.Header) error {
	st, err := system.Lstat(path)
	if err != nil {
		return err
	}
	fflags, err := formatFileFlags(st.Flags())
	if err != nil {
		return err
	}
	if fflags != "" {
		if hdr.PAXRecords == nil {
			hdr.PAXRecords = map[string]string{}
		}
		hdr.PAXRecords[paxSCHILYFflags] = fflags
	}
	return nil
}

func WriteFileFlagsFromTarHeader(path string, hdr *tar.Header) error {
	if fflags, ok := hdr.PAXRecords[paxSCHILYFflags]; ok {
		var set, clear uint32
		set, clear, err := parseFileFlags(fflags)
		if err != nil {
			return err
		}

		// Apply the delta to the existing file flags
		st, err := system.Lstat(path)
		if err != nil {
			return err
		}
		return system.Lchflags(path, (st.Flags() & ^clear)|set)
	}
	return nil
}

func resetImmutable(path string, fi *os.FileInfo) error {
	var flags uint32
	if fi != nil {
		flags = (*fi).Sys().(*syscall.Stat_t).Flags
	} else {
		st, err := system.Lstat(path)
		if err != nil {
			return err
		}
		flags = st.Flags()
	}
	if flags&(system.SF_IMMUTABLE|system.UF_IMMUTABLE) != 0 {
		flags &= ^(system.SF_IMMUTABLE | system.UF_IMMUTABLE)
		return system.Lchflags(path, flags)
	}
	return nil
}
