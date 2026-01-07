//go:build linux && seccomp

package chroot

import (
	"fmt"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	libseccomp "github.com/seccomp/libseccomp-golang"
	"github.com/sirupsen/logrus"
)

// setSeccomp sets the seccomp filter for ourselves and any processes that we'll start.
func setSeccomp(spec *specs.Spec) error {
	logrus.Debugf("setting seccomp configuration")
	if spec.Linux.Seccomp == nil {
		return nil
	}
	mapAction := func(specAction specs.LinuxSeccompAction, errnoRet *uint) libseccomp.ScmpAction {
		switch specAction {
		case specs.ActKill:
			return libseccomp.ActKillThread
		case specs.ActTrap:
			return libseccomp.ActTrap
		case specs.ActErrno:
			action := libseccomp.ActErrno
			if errnoRet != nil {
				action = action.SetReturnCode(int16(*errnoRet))
			}
			return action
		case specs.ActTrace:
			return libseccomp.ActTrace
		case specs.ActAllow:
			return libseccomp.ActAllow
		case specs.ActLog:
			return libseccomp.ActLog
		case specs.ActKillProcess:
			return libseccomp.ActKillProcess
		default:
			logrus.Errorf("unmappable action %v", specAction)
		}
		return libseccomp.ActInvalid
	}
	mapArch := func(specArch specs.Arch) libseccomp.ScmpArch {
		switch specArch {
		case specs.ArchX86:
			return libseccomp.ArchX86
		case specs.ArchX86_64:
			return libseccomp.ArchAMD64
		case specs.ArchX32:
			return libseccomp.ArchX32
		case specs.ArchARM:
			return libseccomp.ArchARM
		case specs.ArchAARCH64:
			return libseccomp.ArchARM64
		case specs.ArchMIPS:
			return libseccomp.ArchMIPS
		case specs.ArchMIPS64:
			return libseccomp.ArchMIPS64
		case specs.ArchMIPS64N32:
			return libseccomp.ArchMIPS64N32
		case specs.ArchMIPSEL:
			return libseccomp.ArchMIPSEL
		case specs.ArchMIPSEL64:
			return libseccomp.ArchMIPSEL64
		case specs.ArchMIPSEL64N32:
			return libseccomp.ArchMIPSEL64N32
		case specs.ArchPPC:
			return libseccomp.ArchPPC
		case specs.ArchPPC64:
			return libseccomp.ArchPPC64
		case specs.ArchPPC64LE:
			return libseccomp.ArchPPC64LE
		case specs.ArchS390:
			return libseccomp.ArchS390
		case specs.ArchS390X:
			return libseccomp.ArchS390X
		case specs.ArchPARISC:
			return libseccomp.ArchPARISC
		case specs.ArchPARISC64:
			return libseccomp.ArchPARISC64
		case specs.ArchRISCV64:
			return libseccomp.ArchRISCV64
		default:
			logrus.Errorf("unmappable arch %v", specArch)
		}
		return libseccomp.ArchInvalid
	}
	mapOp := func(op specs.LinuxSeccompOperator) libseccomp.ScmpCompareOp {
		switch op {
		case specs.OpNotEqual:
			return libseccomp.CompareNotEqual
		case specs.OpLessThan:
			return libseccomp.CompareLess
		case specs.OpLessEqual:
			return libseccomp.CompareLessOrEqual
		case specs.OpEqualTo:
			return libseccomp.CompareEqual
		case specs.OpGreaterEqual:
			return libseccomp.CompareGreaterEqual
		case specs.OpGreaterThan:
			return libseccomp.CompareGreater
		case specs.OpMaskedEqual:
			return libseccomp.CompareMaskedEqual
		default:
			logrus.Errorf("unmappable op %v", op)
		}
		return libseccomp.CompareInvalid
	}

	filter, err := libseccomp.NewFilter(mapAction(spec.Linux.Seccomp.DefaultAction, spec.Linux.Seccomp.DefaultErrnoRet))
	if err != nil {
		return fmt.Errorf("creating seccomp filter with default action %q: %w", spec.Linux.Seccomp.DefaultAction, err)
	}
	for _, arch := range spec.Linux.Seccomp.Architectures {
		if err = filter.AddArch(mapArch(arch)); err != nil {
			return fmt.Errorf("adding architecture %q(%q) to seccomp filter: %w", arch, mapArch(arch), err)
		}
	}
	for _, rule := range spec.Linux.Seccomp.Syscalls {
		scnames := make(map[libseccomp.ScmpSyscall]string)
		for _, name := range rule.Names {
			scnum, err := libseccomp.GetSyscallFromName(name)
			if err != nil {
				logrus.Debugf("error mapping syscall %q to a syscall, ignoring %q rule for %q", name, rule.Action, name)
				continue
			}
			scnames[scnum] = name
		}
		for scnum := range scnames {
			if len(rule.Args) == 0 {
				if err = filter.AddRule(scnum, mapAction(rule.Action, rule.ErrnoRet)); err != nil {
					return fmt.Errorf("adding a rule (%q:%q) to seccomp filter: %w", scnames[scnum], rule.Action, err)
				}
				continue
			}
			var conditions []libseccomp.ScmpCondition
			opsAreAllEquality := true
			for _, arg := range rule.Args {
				condition, err := libseccomp.MakeCondition(arg.Index, mapOp(arg.Op), arg.Value, arg.ValueTwo)
				if err != nil {
					return fmt.Errorf("building a seccomp condition %d:%v:%d:%d: %w", arg.Index, arg.Op, arg.Value, arg.ValueTwo, err)
				}
				if arg.Op != specs.OpEqualTo {
					opsAreAllEquality = false
				}
				conditions = append(conditions, condition)
			}
			if err = filter.AddRuleConditional(scnum, mapAction(rule.Action, rule.ErrnoRet), conditions); err != nil {
				// Okay, if the rules specify multiple equality
				// checks, assume someone thought that they
				// were OR'd, when in fact they're ordinarily
				// supposed to be AND'd.  Break them up into
				// different rules to get that OR effect.
				if len(rule.Args) > 1 && opsAreAllEquality && err.Error() == "two checks on same syscall argument" {
					for i := range conditions {
						if err = filter.AddRuleConditional(scnum, mapAction(rule.Action, rule.ErrnoRet), conditions[i:i+1]); err != nil {
							return fmt.Errorf("adding a conditional rule (%q:%q[%d]) to seccomp filter: %w", scnames[scnum], rule.Action, i, err)
						}
					}
				} else {
					return fmt.Errorf("adding a conditional rule (%q:%q) to seccomp filter: %w", scnames[scnum], rule.Action, err)
				}
			}
		}
	}
	if err = filter.SetNoNewPrivsBit(spec.Process.NoNewPrivileges); err != nil {
		return fmt.Errorf("setting no-new-privileges bit to %v: %w", spec.Process.NoNewPrivileges, err)
	}
	err = filter.Load()
	filter.Release()
	if err != nil {
		return fmt.Errorf("activating seccomp filter: %w", err)
	}
	return nil
}
