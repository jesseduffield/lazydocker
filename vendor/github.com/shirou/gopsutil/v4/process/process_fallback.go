// SPDX-License-Identifier: BSD-3-Clause
//go:build !darwin && !linux && !freebsd && !openbsd && !windows && !solaris && !plan9

package process

import (
	"context"
	"syscall"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/internal/common"
	"github.com/shirou/gopsutil/v4/net"
)

type Signal = syscall.Signal

type MemoryMapsStat struct {
	Path         string `json:"path"`
	Rss          uint64 `json:"rss"`
	Size         uint64 `json:"size"`
	Pss          uint64 `json:"pss"`
	SharedClean  uint64 `json:"sharedClean"`
	SharedDirty  uint64 `json:"sharedDirty"`
	PrivateClean uint64 `json:"privateClean"`
	PrivateDirty uint64 `json:"privateDirty"`
	Referenced   uint64 `json:"referenced"`
	Anonymous    uint64 `json:"anonymous"`
	Swap         uint64 `json:"swap"`
}

type MemoryInfoExStat struct{}

func pidsWithContext(_ context.Context) ([]int32, error) {
	return nil, common.ErrNotImplementedError
}

func ProcessesWithContext(_ context.Context) ([]*Process, error) {
	return nil, common.ErrNotImplementedError
}

func PidExistsWithContext(_ context.Context, _ int32) (bool, error) {
	return false, common.ErrNotImplementedError
}

func (*Process) PpidWithContext(_ context.Context) (int32, error) {
	return 0, common.ErrNotImplementedError
}

func (*Process) NameWithContext(_ context.Context) (string, error) {
	return "", common.ErrNotImplementedError
}

func (*Process) TgidWithContext(_ context.Context) (int32, error) {
	return 0, common.ErrNotImplementedError
}

func (*Process) ExeWithContext(_ context.Context) (string, error) {
	return "", common.ErrNotImplementedError
}

func (*Process) CmdlineWithContext(_ context.Context) (string, error) {
	return "", common.ErrNotImplementedError
}

func (*Process) CmdlineSliceWithContext(_ context.Context) ([]string, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) createTimeWithContext(_ context.Context) (int64, error) {
	return 0, common.ErrNotImplementedError
}

func (*Process) CwdWithContext(_ context.Context) (string, error) {
	return "", common.ErrNotImplementedError
}

func (*Process) StatusWithContext(_ context.Context) ([]string, error) {
	return []string{""}, common.ErrNotImplementedError
}

func (*Process) ForegroundWithContext(_ context.Context) (bool, error) {
	return false, common.ErrNotImplementedError
}

func (*Process) UidsWithContext(_ context.Context) ([]uint32, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) GidsWithContext(_ context.Context) ([]uint32, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) GroupsWithContext(_ context.Context) ([]uint32, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) TerminalWithContext(_ context.Context) (string, error) {
	return "", common.ErrNotImplementedError
}

func (*Process) NiceWithContext(_ context.Context) (int32, error) {
	return 0, common.ErrNotImplementedError
}

func (*Process) IOniceWithContext(_ context.Context) (int32, error) {
	return 0, common.ErrNotImplementedError
}

func (*Process) RlimitWithContext(_ context.Context) ([]RlimitStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) RlimitUsageWithContext(_ context.Context, _ bool) ([]RlimitStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) IOCountersWithContext(_ context.Context) (*IOCountersStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) NumCtxSwitchesWithContext(_ context.Context) (*NumCtxSwitchesStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) NumFDsWithContext(_ context.Context) (int32, error) {
	return 0, common.ErrNotImplementedError
}

func (*Process) NumThreadsWithContext(_ context.Context) (int32, error) {
	return 0, common.ErrNotImplementedError
}

func (*Process) ThreadsWithContext(_ context.Context) (map[int32]*cpu.TimesStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) TimesWithContext(_ context.Context) (*cpu.TimesStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) CPUAffinityWithContext(_ context.Context) ([]int32, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) MemoryInfoWithContext(_ context.Context) (*MemoryInfoStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) MemoryInfoExWithContext(_ context.Context) (*MemoryInfoExStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) PageFaultsWithContext(_ context.Context) (*PageFaultsStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) ChildrenWithContext(_ context.Context) ([]*Process, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) OpenFilesWithContext(_ context.Context) ([]OpenFilesStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) ConnectionsWithContext(_ context.Context) ([]net.ConnectionStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) ConnectionsMaxWithContext(_ context.Context, _ int) ([]net.ConnectionStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) MemoryMapsWithContext(_ context.Context, _ bool) (*[]MemoryMapsStat, error) {
	return nil, common.ErrNotImplementedError
}

func (*Process) SendSignalWithContext(_ context.Context, _ Signal) error {
	return common.ErrNotImplementedError
}

func (*Process) SuspendWithContext(_ context.Context) error {
	return common.ErrNotImplementedError
}

func (*Process) ResumeWithContext(_ context.Context) error {
	return common.ErrNotImplementedError
}

func (*Process) TerminateWithContext(_ context.Context) error {
	return common.ErrNotImplementedError
}

func (*Process) KillWithContext(_ context.Context) error {
	return common.ErrNotImplementedError
}

func (*Process) UsernameWithContext(_ context.Context) (string, error) {
	return "", common.ErrNotImplementedError
}

func (*Process) EnvironWithContext(_ context.Context) ([]string, error) {
	return nil, common.ErrNotImplementedError
}
