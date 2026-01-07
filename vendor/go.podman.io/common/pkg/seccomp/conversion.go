// NOTE: this package has originally been copied from
// github.com/opencontainers/runc and modified to work for other use cases

package seccomp

import (
	"fmt"

	"github.com/opencontainers/runtime-spec/specs-go"
)

var (
	goArchToSeccompArchMap = map[string]Arch{
		"386":         ArchX86,
		"amd64":       ArchX86_64,
		"amd64p32":    ArchX32,
		"arm":         ArchARM,
		"arm64":       ArchAARCH64,
		"mips":        ArchMIPS,
		"mips64":      ArchMIPS64,
		"mips64le":    ArchMIPSEL64,
		"mips64p32":   ArchMIPS64N32,
		"mips64p32le": ArchMIPSEL64N32,
		"mipsle":      ArchMIPSEL,
		"ppc":         ArchPPC,
		"ppc64":       ArchPPC64,
		"ppc64le":     ArchPPC64LE,
		"s390":        ArchS390,
		"s390x":       ArchS390X,
	}
	specArchToLibseccompArchMap = map[specs.Arch]string{
		specs.ArchX86:         "x86",
		specs.ArchX86_64:      "amd64",
		specs.ArchX32:         "x32",
		specs.ArchARM:         "arm",
		specs.ArchAARCH64:     "arm64",
		specs.ArchMIPS:        "mips",
		specs.ArchMIPS64:      "mips64",
		specs.ArchMIPS64N32:   "mips64n32",
		specs.ArchMIPSEL:      "mipsel",
		specs.ArchMIPSEL64:    "mipsel64",
		specs.ArchMIPSEL64N32: "mipsel64n32",
		specs.ArchPPC:         "ppc",
		specs.ArchPPC64:       "ppc64",
		specs.ArchPPC64LE:     "ppc64le",
		specs.ArchS390:        "s390",
		specs.ArchS390X:       "s390x",
	}
	specArchToSeccompArchMap = map[specs.Arch]Arch{
		specs.ArchX86:         ArchX86,
		specs.ArchX86_64:      ArchX86_64,
		specs.ArchX32:         ArchX32,
		specs.ArchARM:         ArchARM,
		specs.ArchAARCH64:     ArchAARCH64,
		specs.ArchMIPS:        ArchMIPS,
		specs.ArchMIPS64:      ArchMIPS64,
		specs.ArchMIPS64N32:   ArchMIPS64N32,
		specs.ArchMIPSEL:      ArchMIPSEL,
		specs.ArchMIPSEL64:    ArchMIPSEL64,
		specs.ArchMIPSEL64N32: ArchMIPSEL64N32,
		specs.ArchPPC:         ArchPPC,
		specs.ArchPPC64:       ArchPPC64,
		specs.ArchPPC64LE:     ArchPPC64LE,
		specs.ArchS390:        ArchS390,
		specs.ArchS390X:       ArchS390X,
	}
	specActionToSeccompActionMap = map[specs.LinuxSeccompAction]Action{
		specs.ActKill: ActKill,
		// TODO: wait for this PR to get merged:
		// https://github.com/opencontainers/runtime-spec/pull/1064
		// specs.ActKillProcess   ActKillProcess,
		// specs.ActKillThread   ActKillThread,
		specs.ActErrno:  ActErrno,
		specs.ActTrap:   ActTrap,
		specs.ActAllow:  ActAllow,
		specs.ActTrace:  ActTrace,
		specs.ActLog:    ActLog,
		specs.ActNotify: ActNotify,
	}
	specOperatorToSeccompOperatorMap = map[specs.LinuxSeccompOperator]Operator{
		specs.OpNotEqual:     OpNotEqual,
		specs.OpLessThan:     OpLessThan,
		specs.OpLessEqual:    OpLessEqual,
		specs.OpEqualTo:      OpEqualTo,
		specs.OpGreaterEqual: OpGreaterEqual,
		specs.OpGreaterThan:  OpGreaterThan,
		specs.OpMaskedEqual:  OpMaskedEqual,
	}
)

// GoArchToSeccompArch converts a runtime.GOARCH to a seccomp `Arch`. The
// function returns an error if the architecture conversion is not supported.
func GoArchToSeccompArch(goArch string) (Arch, error) {
	arch, ok := goArchToSeccompArchMap[goArch]
	if !ok {
		return "", fmt.Errorf("unsupported go arch provided: %s", goArch)
	}
	return arch, nil
}

// specToSeccomp converts a `LinuxSeccomp` spec into a `Seccomp` struct.
func specToSeccomp(spec *specs.LinuxSeccomp) (*Seccomp, error) {
	res := &Seccomp{
		Syscalls: []*Syscall{},
	}

	for _, arch := range spec.Architectures {
		newArch, err := specArchToSeccompArch(arch)
		if err != nil {
			return nil, fmt.Errorf("convert spec arch: %w", err)
		}
		res.Architectures = append(res.Architectures, newArch)
	}

	// Convert default action
	newDefaultAction, err := specActionToSeccompAction(spec.DefaultAction)
	if err != nil {
		return nil, fmt.Errorf("convert default action: %w", err)
	}
	res.DefaultAction = newDefaultAction
	res.DefaultErrnoRet = spec.DefaultErrnoRet

	// Loop through all syscall blocks and convert them to the internal format
	for _, call := range spec.Syscalls {
		newAction, err := specActionToSeccompAction(call.Action)
		if err != nil {
			return nil, fmt.Errorf("convert action: %w", err)
		}

		for _, name := range call.Names {
			newCall := Syscall{
				Name:     name,
				Action:   newAction,
				ErrnoRet: call.ErrnoRet,
				Args:     []*Arg{},
			}

			// Loop through all the arguments of the syscall and convert them
			for _, arg := range call.Args {
				newOp, err := specOperatorToSeccompOperator(arg.Op)
				if err != nil {
					return nil, fmt.Errorf("convert operator: %w", err)
				}

				newArg := Arg{
					Index:    arg.Index,
					Value:    arg.Value,
					ValueTwo: arg.ValueTwo,
					Op:       newOp,
				}

				newCall.Args = append(newCall.Args, &newArg)
			}
			res.Syscalls = append(res.Syscalls, &newCall)
		}
	}

	return res, nil
}

// specArchToLibseccompArch converts a spec arch into a libseccomp one.
func specArchToLibseccompArch(arch specs.Arch) (string, error) {
	if res, ok := specArchToLibseccompArchMap[arch]; ok {
		return res, nil
	}
	return "", fmt.Errorf(
		"architecture %q is not valid for libseccomp", arch,
	)
}

// specArchToSeccompArch converts a spec arch into an internal one.
func specArchToSeccompArch(arch specs.Arch) (Arch, error) {
	if res, ok := specArchToSeccompArchMap[arch]; ok {
		return res, nil
	}
	return "", fmt.Errorf("architecture %q is not valid", arch)
}

// specActionToSeccompAction converts a spec action into a seccomp one.
func specActionToSeccompAction(action specs.LinuxSeccompAction) (Action, error) {
	if res, ok := specActionToSeccompActionMap[action]; ok {
		return res, nil
	}
	return "", fmt.Errorf(
		"spec action %q is not valid internal action", action,
	)
}

// specOperatorToSeccompOperator converts a spec operator into a seccomp one.
func specOperatorToSeccompOperator(operator specs.LinuxSeccompOperator) (Operator, error) {
	if op, ok := specOperatorToSeccompOperatorMap[operator]; ok {
		return op, nil
	}
	return "", fmt.Errorf(
		"spec operator %q is not a valid internal operator", operator,
	)
}
