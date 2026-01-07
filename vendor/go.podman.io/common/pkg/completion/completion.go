package completion

import (
	"bufio"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"go.podman.io/common/pkg/capabilities"
)

// FlagCompletions - hold flag completion functions to be applied later with CompleteCommandFlags().
type FlagCompletions map[string]func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

// CompleteCommandFlags - Add completion functions for each flagname in FlagCompletions.
func CompleteCommandFlags(cmd *cobra.Command, flags FlagCompletions) {
	for flagName, completionFunc := range flags {
		_ = cmd.RegisterFlagCompletionFunc(flagName, completionFunc)
	}
}

/* Autocomplete Functions for cobra ValidArgsFunction */

// AutocompleteNone - Block the default shell completion (no paths).
func AutocompleteNone(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// AutocompleteDefault - Use the default shell completion,
// allows path completion.
func AutocompleteDefault(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveDefault
}

// AutocompleteCapabilities - Autocomplete linux capabilities options.
// Used by --cap-add and --cap-drop.
func AutocompleteCapabilities(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	caps := capabilities.AllCapabilities()

	// convertCase will convert a string to lowercase only if the user input is lowercase
	convertCase := func(s string) string { return s }
	if len(toComplete) > 0 && unicode.IsLower(rune(toComplete[0])) {
		convertCase = strings.ToLower
	}

	// offset is used to trim "CAP_" if the user doesn't type CA... or ca...
	offset := 0
	if !strings.HasPrefix(toComplete, convertCase("CA")) {
		// setting the offset to 4 is safe since each cap starts with CAP_
		offset = 4
	}

	completions := make([]string, 0, len(caps))
	for _, cap := range caps {
		completions = append(completions, convertCase(cap)[offset:])
	}

	// add ALL here which is also a valid argument
	completions = append(completions, convertCase(capabilities.All))
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// autocompleteSubIDName - autocomplete the names in /etc/subuid or /etc/subgid.
func autocompleteSubIDName(filename string) ([]string, cobra.ShellCompDirective) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	defer file.Close()

	var names []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, _, _ := strings.Cut(scanner.Text(), ":")
		names = append(names, name)
	}
	if err = scanner.Err(); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// AutocompleteSubgidName - Autocomplete subgidname based on the names in the /etc/subgid file.
func AutocompleteSubgidName(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return autocompleteSubIDName("/etc/subgid")
}

// AutocompleteSubuidName - Autocomplete subuidname based on the names in the /etc/subuid file.
func AutocompleteSubuidName(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return autocompleteSubIDName("/etc/subuid")
}

// AutocompletePlatform - Autocomplete platform supported by container engines.
func AutocompletePlatform(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	completions := []string{
		"linux/386",
		"linux/amd64",
		"linux/arm",
		"linux/arm64",
		"linux/ppc64",
		"linux/ppc64le",
		"linux/mips",
		"linux/mipsle",
		"linux/mips64",
		"linux/mips64le",
		"linux/riscv64",
		"linux/s390x",
		"windows/386",
		"windows/amd64",
		"windows/arm",
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// AutocompleteArch - Autocomplete architectures supported by container engines.
func AutocompleteArch(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	completions := []string{
		"386",
		"amd64",
		"arm",
		"arm64",
		"ppc64",
		"ppc64le",
		"mips",
		"mipsle",
		"mips64",
		"mips64le",
		"riscv64",
		"s390x",
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// AutocompleteOS - Autocomplete OS supported by container engines.
func AutocompleteOS(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	completions := []string{"linux", "windows"}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// AutocompleteJSONFormat - Autocomplete format flag option.
// -> "json".
func AutocompleteJSONFormat(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"json"}, cobra.ShellCompDirectiveNoFileComp
}

// AutocompleteOneArg - Autocomplete one random arg.
func AutocompleteOneArg(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 1 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
