//go:build seccomp

// NOTE: this package has originally been copied from
// github.com/opencontainers/runc and modified to work for other use cases

package seccomp

import (
	"errors"
	"fmt"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	libseccomp "github.com/seccomp/libseccomp-golang"
	"golang.org/x/sys/unix"
)

// NOTE: this package has originally been copied from
// github.com/opencontainers/runc and modified to work for other use cases

var (
	// ErrSpecNil is a possible return error from BuildFilter() and occurs if
	// the provided spec is nil.
	ErrSpecNil = errors.New("spec is nil")

	// ErrSpecEmpty is a possible return error from BuildFilter() and occurs if
	// the provided spec has neither a DefaultAction nor any syscalls.
	ErrSpecEmpty = errors.New("spec contains neither a default action nor any syscalls")
)

// BuildFilter does a basic validation for the provided seccomp profile
// string and returns a filter for it.
func BuildFilter(spec *specs.LinuxSeccomp) (*libseccomp.ScmpFilter, error) {
	// Sanity checking to allow consumers to act accordingly
	if spec == nil {
		return nil, ErrSpecNil
	}
	if spec.DefaultAction == "" && len(spec.Syscalls) == 0 {
		return nil, ErrSpecEmpty
	}

	profile, err := specToSeccomp(spec)
	if err != nil {
		return nil, fmt.Errorf("convert spec to seccomp profile: %w", err)
	}

	defaultAction, err := toAction(profile.DefaultAction, profile.DefaultErrnoRet)
	if err != nil {
		return nil, fmt.Errorf("convert default action %s: %w", profile.DefaultAction, err)
	}

	filter, err := libseccomp.NewFilter(defaultAction)
	if err != nil {
		return nil, fmt.Errorf("create filter for default action %s: %w", defaultAction, err)
	}

	// Add extra architectures
	for _, arch := range spec.Architectures {
		libseccompArch, err := specArchToLibseccompArch(arch)
		if err != nil {
			return nil, fmt.Errorf("convert spec arch: %w", err)
		}

		scmpArch, err := libseccomp.GetArchFromString(libseccompArch)
		if err != nil {
			return nil, fmt.Errorf("validate Seccomp architecture %s: %w", arch, err)
		}

		if err := filter.AddArch(scmpArch); err != nil {
			return nil, fmt.Errorf("add architecture to seccomp filter: %w", err)
		}
	}

	// Unset no new privs bit
	if err := filter.SetNoNewPrivsBit(false); err != nil {
		return nil, fmt.Errorf("set no new privileges flag: %w", err)
	}

	// Add a rule for each syscall
	for _, call := range profile.Syscalls {
		if call == nil {
			return nil, errors.New("encountered nil syscall while initializing seccomp")
		}

		if err = matchSyscall(filter, call); err != nil {
			return nil, fmt.Errorf("filter matches syscall: %w", err)
		}
	}

	return filter, nil
}

func matchSyscall(filter *libseccomp.ScmpFilter, call *Syscall) error {
	if call == nil || filter == nil {
		return errors.New("cannot use nil as syscall to block")
	}

	if call.Name == "" {
		return errors.New("empty string is not a valid syscall")
	}

	// If we can't resolve the syscall, assume it's not supported on this kernel
	// Ignore it, don't error out
	callNum, err := libseccomp.GetSyscallFromName(call.Name)
	if err != nil {
		return nil
	}

	// Convert the call's action to the libseccomp equivalent
	callAct, err := toAction(call.Action, call.ErrnoRet)
	if err != nil {
		return fmt.Errorf("convert action %s: %w", call.Action, err)
	}

	// Unconditional match - just add the rule
	if len(call.Args) == 0 {
		if err = filter.AddRule(callNum, callAct); err != nil {
			return fmt.Errorf("add seccomp filter rule for syscall %s: %w", call.Name, err)
		}
	} else {
		// Linux system calls can have at most 6 arguments
		const syscallMaxArguments int = 6

		// If two or more arguments have the same condition,
		// Revert to old behavior, adding each condition as a separate rule
		argCounts := make([]uint, syscallMaxArguments)
		conditions := []libseccomp.ScmpCondition{}

		for _, cond := range call.Args {
			newCond, err := toCondition(cond)
			if err != nil {
				return fmt.Errorf("create seccomp syscall condition for syscall %s: %w", call.Name, err)
			}

			argCounts[cond.Index]++

			conditions = append(conditions, newCond)
		}

		hasMultipleArgs := false
		for _, count := range argCounts {
			if count > 1 {
				hasMultipleArgs = true
				break
			}
		}

		if hasMultipleArgs {
			// Revert to old behavior
			// Add each condition attached to a separate rule
			for _, cond := range conditions {
				condArr := []libseccomp.ScmpCondition{cond}

				if err = filter.AddRuleConditional(callNum, callAct, condArr); err != nil {
					return fmt.Errorf("add seccomp rule for syscall %s: %w", call.Name, err)
				}
			}
		} else if err = filter.AddRuleConditional(callNum, callAct, conditions); err != nil {
			// No conditions share same argument
			// Use new, proper behavior
			return fmt.Errorf("add seccomp rule for syscall %s: %w", call.Name, err)
		}
	}

	return nil
}

// toAction converts an internal `Action` type to a `libseccomp.ScmpAction`
// type.
func toAction(act Action, errnoRet *uint) (libseccomp.ScmpAction, error) {
	switch act {
	case ActKill:
		// lint was not passing until this was changed from ActKill to ActKilThread.
		return libseccomp.ActKillThread, nil
	case ActKillProcess:
		return libseccomp.ActKillProcess, nil
	case ActErrno:
		if errnoRet != nil {
			return libseccomp.ActErrno.SetReturnCode(int16(*errnoRet)), nil
		}
		return libseccomp.ActErrno.SetReturnCode(int16(unix.EPERM)), nil
	case ActTrap:
		return libseccomp.ActTrap, nil
	case ActAllow:
		return libseccomp.ActAllow, nil
	case ActTrace:
		if errnoRet != nil {
			return libseccomp.ActTrace.SetReturnCode(int16(*errnoRet)), nil
		}
		return libseccomp.ActTrace.SetReturnCode(int16(unix.EPERM)), nil
	case ActLog:
		return libseccomp.ActLog, nil
	default:
		return libseccomp.ActInvalid, fmt.Errorf("invalid action %s", act)
	}
}

// toCondition converts an internal `Arg` type to a `libseccomp.ScmpCondition`
// type.
func toCondition(arg *Arg) (cond libseccomp.ScmpCondition, err error) {
	if arg == nil {
		return cond, errors.New("cannot convert nil to syscall condition")
	}

	op, err := toCompareOp(arg.Op)
	if err != nil {
		return cond, fmt.Errorf("convert compare operator: %w", err)
	}

	condition, err := libseccomp.MakeCondition(
		arg.Index, op, arg.Value, arg.ValueTwo,
	)
	if err != nil {
		return cond, fmt.Errorf("make condition: %w", err)
	}

	return condition, nil
}

// toCompareOp converts an internal `Operator` type to a
// `libseccomp.ScmpCompareOp`.
func toCompareOp(op Operator) (libseccomp.ScmpCompareOp, error) {
	switch op {
	case OpEqualTo:
		return libseccomp.CompareEqual, nil
	case OpNotEqual:
		return libseccomp.CompareNotEqual, nil
	case OpGreaterThan:
		return libseccomp.CompareGreater, nil
	case OpGreaterEqual:
		return libseccomp.CompareGreaterEqual, nil
	case OpLessThan:
		return libseccomp.CompareLess, nil
	case OpLessEqual:
		return libseccomp.CompareLessOrEqual, nil
	case OpMaskedEqual:
		return libseccomp.CompareMaskedEqual, nil
	default:
		return libseccomp.CompareInvalid, fmt.Errorf("invalid operator %s", op)
	}
}
