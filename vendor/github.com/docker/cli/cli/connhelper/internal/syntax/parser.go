// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

// LangVariant describes a shell language variant to use when tokenizing and
// parsing shell code. The zero value is [LangBash].
type LangVariant int

const (
	// LangBash corresponds to the GNU Bash language, as described in its
	// manual at https://www.gnu.org/software/bash/manual/bash.html.
	//
	// We currently follow Bash version 5.2.
	//
	// Its string representation is "bash".
	LangBash LangVariant = iota

	// LangPOSIX corresponds to the POSIX Shell language, as described at
	// https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html.
	//
	// Its string representation is "posix" or "sh".
	LangPOSIX

	// LangMirBSDKorn corresponds to the MirBSD Korn Shell, also known as
	// mksh, as described at http://www.mirbsd.org/htman/i386/man1/mksh.htm.
	// Note that it shares some features with Bash, due to the shared
	// ancestry that is ksh.
	//
	// We currently follow mksh version 59.
	//
	// Its string representation is "mksh".
	LangMirBSDKorn

	// LangBats corresponds to the Bash Automated Testing System language,
	// as described at https://github.com/bats-core/bats-core. Note that
	// it's just a small extension of the Bash language.
	//
	// Its string representation is "bats".
	LangBats

	// LangAuto corresponds to automatic language detection,
	// commonly used by end-user applications like shfmt,
	// which can guess a file's language variant given its filename or shebang.
	//
	// At this time, [Variant] does not support LangAuto.
	LangAuto
)

func (l LangVariant) String() string {
	switch l {
	case LangBash:
		return "bash"
	case LangPOSIX:
		return "posix"
	case LangMirBSDKorn:
		return "mksh"
	case LangBats:
		return "bats"
	case LangAuto:
		return "auto"
	}
	return "unknown shell language variant"
}

// IsKeyword returns true if the given word is part of the language keywords.
func IsKeyword(word string) bool {
	// This list has been copied from the bash 5.1 source code, file y.tab.c +4460
	switch word {
	case
		"!",
		"[[", // only if COND_COMMAND is defined
		"]]", // only if COND_COMMAND is defined
		"case",
		"coproc", // only if COPROCESS_SUPPORT is defined
		"do",
		"done",
		"else",
		"esac",
		"fi",
		"for",
		"function",
		"if",
		"in",
		"select", // only if SELECT_COMMAND is defined
		"then",
		"time", // only if COMMAND_TIMING is defined
		"until",
		"while",
		"{",
		"}":
		return true
	}
	return false
}
