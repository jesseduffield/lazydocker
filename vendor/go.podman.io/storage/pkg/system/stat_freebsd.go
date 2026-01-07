package system

import "syscall"

type platformStatT struct {
	flags uint32
}

// Flags return file flags if supported or zero otherwise
func (s StatT) Flags() uint32 {
	return s.flags
}

// fromStatT converts a syscall.Stat_t type to a system.Stat_t type
func fromStatT(s *syscall.Stat_t) (*StatT, error) {
	st := &StatT{
		size: s.Size,
		mode: uint32(s.Mode),
		uid:  s.Uid,
		gid:  s.Gid,
		rdev: uint64(s.Rdev),
		mtim: s.Mtimespec,
		dev:  s.Dev,
	}
	st.flags = s.Flags
	st.dev = s.Dev
	return st, nil
}
