package imagebuilder

// This file contains the dispatchers for each command. Note that
// `nullDispatch` is not actually a command, but support for commands we parse
// but do nothing with.
//
// See evaluator.go for a higher level discussion of the whole evaluator
// package.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	docker "github.com/fsouza/go-dockerclient"

	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/openshift/imagebuilder/internal"
	"github.com/openshift/imagebuilder/signal"
	"github.com/openshift/imagebuilder/strslice"
	"go.podman.io/storage/pkg/regexp"

	buildkitcommand "github.com/moby/buildkit/frontend/dockerfile/command"
	buildkitparser "github.com/moby/buildkit/frontend/dockerfile/parser"
	buildkitshell "github.com/moby/buildkit/frontend/dockerfile/shell"
)

var (
	obRgex = regexp.Delayed(`(?i)^\s*ONBUILD\s*`)
)

var localspec = platforms.DefaultSpec()

// https://docs.docker.com/engine/reference/builder/#automatic-platform-args-in-the-global-scope
var builtinArgDefaults = map[string]string{
	"TARGETPLATFORM": localspec.OS + "/" + localspec.Architecture,
	"TARGETOS":       localspec.OS,
	"TARGETARCH":     localspec.Architecture,
	"TARGETVARIANT":  "",
	"BUILDPLATFORM":  localspec.OS + "/" + localspec.Architecture,
	"BUILDOS":        localspec.OS,
	"BUILDARCH":      localspec.Architecture,
	"BUILDVARIANT":   "",
}

// ENV foo bar
//
// Sets the environment variable foo to bar, also makes interpolation
// in the dockerfile available from the next statement on via ${foo}.
func env(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) == 0 {
		return errAtLeastOneArgument("ENV")
	}

	if len(args)%2 != 0 {
		// should never get here, but just in case
		return errTooManyArguments("ENV")
	}

	// TODO/FIXME/NOT USED
	// Just here to show how to use the builder flags stuff within the
	// context of a builder command. Will remove once we actually add
	// a builder command to something!
	/*
		flBool1 := b.flags.AddBool("bool1", false)
		flStr1 := b.flags.AddString("str1", "HI")

		if err := b.flags.Parse(); err != nil {
			return err
		}

		fmt.Printf("Bool1:%v\n", flBool1)
		fmt.Printf("Str1:%v\n", flStr1)
	*/

	for j := 0; j+1 < len(args); j += 2 {
		// name  ==> args[j]
		// value ==> args[j+1]
		newVar := []string{args[j] + "=" + args[j+1]}
		b.RunConfig.Env = mergeEnv(b.RunConfig.Env, newVar)
		b.Env = mergeEnv(b.Env, newVar)
	}

	return nil
}

// MAINTAINER some text <maybe@an.email.address>
//
// Sets the maintainer metadata.
func maintainer(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) != 1 {
		return errExactlyOneArgument("MAINTAINER")
	}
	b.Author = args[0]
	return nil
}

// LABEL some json data describing the image
//
// Sets the Label variable foo to bar,
func label(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) == 0 {
		return errAtLeastOneArgument("LABEL")
	}
	if len(args)%2 != 0 {
		// should never get here, but just in case
		return errTooManyArguments("LABEL")
	}

	if b.RunConfig.Labels == nil {
		b.RunConfig.Labels = map[string]string{}
	}

	for j := 0; j < len(args); j++ {
		// name  ==> args[j]
		// value ==> args[j+1]
		b.RunConfig.Labels[args[j]] = args[j+1]
		j++
	}
	return nil
}

func processHereDocs(instruction, originalInstruction string, heredocs []buildkitparser.Heredoc, args []string) ([]File, error) {
	var files []File
	for _, heredoc := range heredocs {
		var err error
		content := heredoc.Content
		if heredoc.Chomp {
			content = buildkitparser.ChompHeredocContent(content)
		}
		if heredoc.Expand && !strings.EqualFold(instruction, buildkitcommand.Run) {
			shlex := buildkitshell.NewLex('\\')
			shlex.RawQuotes = true
			shlex.RawEscapes = true
			content, _, err = shlex.ProcessWord(content, internal.EnvironmentSlice(args))
			if err != nil {
				return nil, err
			}
		}
		file := File{
			Data: content,
			Name: heredoc.Name,
		}
		files = append(files, file)
	}
	return files, nil
}

// ADD foo /path
//
// Add the file 'foo' to '/path'. Tarball and Remote URL (git, http) handling
// exist here. If you do not wish to have this automatic handling, use COPY.
func add(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) < 2 {
		return errAtLeastTwoArgument("ADD")
	}
	var chown string
	var chmod string
	var checksum string
	var keepGitDir bool
	var link bool
	var excludes []string
	last := len(args) - 1
	dest := makeAbsolute(args[last], b.RunConfig.WorkingDir)
	filteredUserArgs := make(map[string]string)
	for k, v := range b.Args {
		if _, ok := b.AllowedArgs[k]; ok {
			filteredUserArgs[k] = v
		}
	}
	userArgs := mergeEnv(envMapAsSlice(filteredUserArgs), b.Env)
	for _, a := range flagArgs {
		arg, err := ProcessWord(a, userArgs)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(arg, "--chown="):
			chown = strings.TrimPrefix(arg, "--chown=")
			if chown == "" {
				return fmt.Errorf("no value specified for --chown=")
			}
		case strings.HasPrefix(arg, "--chmod="):
			chmod = strings.TrimPrefix(arg, "--chmod=")
			err = checkChmodConversion(chmod)
			if err != nil {
				return err
			}
		case strings.HasPrefix(arg, "--checksum="):
			checksum = strings.TrimPrefix(arg, "--checksum=")
			if checksum == "" {
				return fmt.Errorf("no value specified for --checksum=")
			}
		case arg == "--link", arg == "--link=true":
			link = true
		case arg == "--link=false":
			link = false
		case arg == "--keep-git-dir", arg == "--keep-git-dir=true":
			keepGitDir = true
		case arg == "--keep-git-dir=false":
			keepGitDir = false
		case strings.HasPrefix(arg, "--exclude="):
			exclude := strings.TrimPrefix(arg, "--exclude=")
			if exclude == "" {
				return fmt.Errorf("no value specified for --exclude=")
			}
			excludes = append(excludes, exclude)
		default:
			return fmt.Errorf("ADD only supports the --chmod=<permissions>, --chown=<uid:gid>, --checksum=<checksum>, --link, --keep-git-dir, and --exclude=<pattern> flags")
		}
	}
	files, err := processHereDocs(buildkitcommand.Add, original, heredocs, userArgs)
	if err != nil {
		return err
	}
	b.PendingCopies = append(b.PendingCopies, Copy{
		Src:        args[0:last],
		Dest:       dest,
		Download:   true,
		Chown:      chown,
		Chmod:      chmod,
		Checksum:   checksum,
		Files:      files,
		KeepGitDir: keepGitDir,
		Link:       link,
		Excludes:   excludes,
	})
	return nil
}

// COPY foo /path
//
// Same as 'ADD' but without the tar and remote url handling.
func dispatchCopy(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) < 2 {
		return errAtLeastTwoArgument("COPY")
	}
	last := len(args) - 1
	dest := makeAbsolute(args[last], b.RunConfig.WorkingDir)
	var chown string
	var chmod string
	var from string
	var link bool
	var parents bool
	var excludes []string
	filteredUserArgs := make(map[string]string)
	for k, v := range b.Args {
		if _, ok := b.AllowedArgs[k]; ok {
			filteredUserArgs[k] = v
		}
	}
	userArgs := mergeEnv(envMapAsSlice(filteredUserArgs), b.Env)
	for _, a := range flagArgs {
		arg, err := ProcessWord(a, userArgs)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(arg, "--chown="):
			chown = strings.TrimPrefix(arg, "--chown=")
			if chown == "" {
				return fmt.Errorf("no value specified for --chown=")
			}
		case strings.HasPrefix(arg, "--chmod="):
			chmod = strings.TrimPrefix(arg, "--chmod=")
			err = checkChmodConversion(chmod)
			if err != nil {
				return err
			}
		case strings.HasPrefix(arg, "--from="):
			from = strings.TrimPrefix(arg, "--from=")
			if from == "" {
				return fmt.Errorf("no value specified for --from=")
			}
		case arg == "--link", arg == "--link=true":
			link = true
		case arg == "--link=false":
			link = false
		case arg == "--parents", arg == "--parents=true":
			parents = true
		case arg == "--parents=false":
			parents = false
		case strings.HasPrefix(arg, "--exclude="):
			exclude := strings.TrimPrefix(arg, "--exclude=")
			if exclude == "" {
				return fmt.Errorf("no value specified for --exclude=")
			}
			excludes = append(excludes, exclude)
		default:
			return fmt.Errorf("COPY only supports the --chmod=<permissions>, --chown=<uid:gid>, --from=<image|stage>, --link, --parents, and --exclude=<pattern> flags")
		}
	}
	files, err := processHereDocs(buildkitcommand.Copy, original, heredocs, userArgs)
	if err != nil {
		return err
	}
	b.PendingCopies = append(b.PendingCopies, Copy{
		From:     from,
		Src:      args[0:last],
		Dest:     dest,
		Download: false,
		Chown:    chown,
		Chmod:    chmod,
		Files:    files,
		Link:     link,
		Parents:  parents,
		Excludes: excludes,
	})
	return nil
}

// FROM imagename
//
// This sets the image the dockerfile will build on top of.
func from(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	switch {
	case len(args) == 1:
	case len(args) == 3 && len(args[0]) > 0 && strings.EqualFold(args[1], "as") && len(args[2]) > 0:

	default:
		return fmt.Errorf("FROM requires either one argument, or three: FROM <source> [AS <name>]")
	}

	name := args[0]

	// Support ARG before FROM
	filteredUserArgs := make(map[string]string)
	for k, v := range b.UserArgs {
		for _, a := range b.GlobalAllowedArgs {
			if a == k {
				filteredUserArgs[k] = v
			}
		}
	}
	userArgs := mergeEnv(envMapAsSlice(filteredUserArgs), b.Env)
	userArgs = mergeEnv(envMapAsSlice(b.BuiltinArgDefaults), userArgs)
	userArgs = mergeEnv(envMapAsSlice(builtinArgDefaults), userArgs)
	userArgs = mergeEnv(envMapAsSlice(b.HeadingArgs), userArgs)
	var err error
	if name, err = ProcessWord(name, userArgs); err != nil {
		return err
	}

	// Windows cannot support a container with no base image.
	if name == NoBaseImageSpecifier {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("Windows does not support FROM scratch")
		}
	}
	for _, a := range flagArgs {
		arg, err := ProcessWord(a, userArgs)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(arg, "--platform="):
			platformString := strings.TrimPrefix(arg, "--platform=")
			if platformString == "" {
				return fmt.Errorf("no value specified for --platform=")
			}
			b.Platform = platformString
		default:
			return fmt.Errorf("FROM only supports the --platform flag")
		}
	}
	b.RunConfig.Image = name
	// TODO: handle onbuild
	return nil
}

// ONBUILD RUN echo yo
//
// ONBUILD triggers run when the image is used in a FROM statement.
//
// ONBUILD handling has a lot of special-case functionality, the heading in
// evaluator.go and comments around dispatch() in the same file explain the
// special cases. search for 'OnBuild' in internals.go for additional special
// cases.
func onbuild(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) == 0 {
		return errAtLeastOneArgument("ONBUILD")
	}

	triggerInstruction := strings.ToUpper(strings.TrimSpace(args[0]))
	switch triggerInstruction {
	case "ONBUILD":
		return fmt.Errorf("Chaining ONBUILD via `ONBUILD ONBUILD` isn't allowed")
	case "MAINTAINER", "FROM":
		return fmt.Errorf("%s isn't allowed as an ONBUILD trigger", triggerInstruction)
	}

	original = obRgex.ReplaceAllString(original, "")

	b.RunConfig.OnBuild = append(b.RunConfig.OnBuild, original)
	return nil
}

// WORKDIR /tmp
//
// Set the working directory for future RUN/CMD/etc statements.
func workdir(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) != 1 {
		return errExactlyOneArgument("WORKDIR")
	}

	// This is from the Dockerfile and will not necessarily be in platform
	// specific semantics, hence ensure it is converted.
	workdir := filepath.FromSlash(args[0])

	if !filepath.IsAbs(workdir) {
		current := filepath.FromSlash(b.RunConfig.WorkingDir)
		workdir = filepath.Join(string(os.PathSeparator), current, workdir)
	}

	if workdir != string(os.PathSeparator) {
		workdir = strings.TrimSuffix(workdir, string(os.PathSeparator))
	}

	b.RunConfig.WorkingDir = workdir
	return nil
}

// RUN some command yo
//
// run a command and commit the image. Args are automatically prepended with
// 'sh -c' under linux or 'cmd /S /C' under Windows, in the event there is
// only one argument. The difference in processing:
//
// RUN echo hi          # sh -c echo hi       (Linux)
// RUN echo hi          # cmd /S /C echo hi   (Windows)
// RUN [ "echo", "hi" ] # echo hi
func run(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if b.RunConfig.Image == "" {
		return fmt.Errorf("Please provide a source image with `from` prior to run")
	}

	args = handleJSONArgs(args, attributes)

	var mounts []string
	var network string
	filteredUserArgs := make(map[string]string)
	for k, v := range b.Args {
		if _, ok := b.AllowedArgs[k]; ok {
			filteredUserArgs[k] = v
		}
	}
	userArgs := mergeEnv(envMapAsSlice(filteredUserArgs), b.Env)
	for _, a := range flagArgs {
		arg, err := ProcessWord(a, userArgs)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(arg, "--mount="):
			mount := strings.TrimPrefix(arg, "--mount=")
			if mount == "" {
				return fmt.Errorf("no value specified for --mount=")
			}
			mounts = append(mounts, mount)
		case strings.HasPrefix(arg, "--network="):
			network = strings.TrimPrefix(arg, "--network=")
			if network == "" {
				return fmt.Errorf("no value specified for --network=")
			}
		default:
			return fmt.Errorf("RUN only supports the --mount and --network flag")
		}
	}

	files, err := processHereDocs(buildkitcommand.Run, original, heredocs, userArgs)
	if err != nil {
		return err
	}

	run := Run{
		Args:    args,
		Mounts:  mounts,
		Network: network,
		Files:   files,
	}

	if !attributes["json"] {
		run.Shell = true
	}
	b.PendingRuns = append(b.PendingRuns, run)
	return nil
}

// CMD foo
//
// Set the default command to run in the container (which may be empty).
// Argument handling is the same as RUN.
func cmd(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	cmdSlice := handleJSONArgs(args, attributes)

	if !attributes["json"] {
		if runtime.GOOS != "windows" {
			cmdSlice = append([]string{"/bin/sh", "-c"}, cmdSlice...)
		} else {
			cmdSlice = append([]string{"cmd", "/S", "/C"}, cmdSlice...)
		}
	}

	b.RunConfig.Cmd = strslice.StrSlice(cmdSlice)
	if len(args) != 0 {
		b.CmdSet = true
	}
	return nil
}

// ENTRYPOINT /usr/sbin/nginx
//
// Set the entrypoint (which defaults to sh -c on linux, or cmd /S /C on Windows) to
// /usr/sbin/nginx. Will accept the CMD as the arguments to /usr/sbin/nginx.
//
// Handles command processing similar to CMD and RUN, only b.RunConfig.Entrypoint
// is initialized at NewBuilder time instead of through argument parsing.
func entrypoint(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	parsed := handleJSONArgs(args, attributes)

	switch {
	case attributes["json"]:
		// ENTRYPOINT ["echo", "hi"]
		b.RunConfig.Entrypoint = strslice.StrSlice(parsed)
	case len(parsed) == 0:
		// ENTRYPOINT []
		b.RunConfig.Entrypoint = nil
	default:
		// ENTRYPOINT echo hi
		if runtime.GOOS != "windows" {
			b.RunConfig.Entrypoint = strslice.StrSlice{"/bin/sh", "-c", parsed[0]}
		} else {
			b.RunConfig.Entrypoint = strslice.StrSlice{"cmd", "/S", "/C", parsed[0]}
		}
	}

	// when setting the entrypoint if a CMD was not explicitly set then
	// set the command to nil
	if !b.CmdSet {
		b.RunConfig.Cmd = nil
	}
	return nil
}

// EXPOSE 6667/tcp 7000/tcp
//
// Expose ports for links and port mappings. This all ends up in
// b.RunConfig.ExposedPorts for runconfig.
func expose(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) == 0 {
		return errAtLeastOneArgument("EXPOSE")
	}

	if b.RunConfig.ExposedPorts == nil {
		b.RunConfig.ExposedPorts = make(map[docker.Port]struct{})
	}

	existing := map[string]struct{}{}
	for k := range b.RunConfig.ExposedPorts {
		existing[k.Port()+"/"+k.Proto()] = struct{}{}
	}

	for _, port := range args {
		dp := docker.Port(port)
		if _, exists := existing[dp.Port()+"/"+dp.Proto()]; !exists {
			b.RunConfig.ExposedPorts[docker.Port(fmt.Sprintf("%s/%s", dp.Port(), dp.Proto()))] = struct{}{}
		}
	}
	return nil
}

// USER foo
//
// Set the user to 'foo' for future commands and when running the
// ENTRYPOINT/CMD at container run time.
func user(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) != 1 {
		return errExactlyOneArgument("USER")
	}

	b.RunConfig.User = args[0]
	return nil
}

// VOLUME /foo
//
// Expose the volume /foo for use. Will also accept the JSON array form.
func volume(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) == 0 {
		return errAtLeastOneArgument("VOLUME")
	}

	if b.RunConfig.Volumes == nil {
		b.RunConfig.Volumes = map[string]struct{}{}
	}
	for _, v := range args {
		v = strings.TrimSpace(v)
		if v == "" {
			return fmt.Errorf("Volume specified can not be an empty string")
		}
		b.RunConfig.Volumes[v] = struct{}{}
		b.PendingVolumes.Add(v)
	}
	return nil
}

// STOPSIGNAL signal
//
// Set the signal that will be used to kill the container.
func stopSignal(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) != 1 {
		return errExactlyOneArgument("STOPSIGNAL")
	}

	sig := args[0]
	if err := signal.CheckSignal(sig); err != nil {
		return err
	}

	b.RunConfig.StopSignal = sig
	return nil
}

// HEALTHCHECK foo
//
// Set the default healthcheck command to run in the container (which may be empty).
// Argument handling is the same as RUN.
func healthcheck(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	if len(args) == 0 {
		return errAtLeastOneArgument("HEALTHCHECK")
	}
	typ := strings.ToUpper(args[0])
	args = args[1:]
	if typ == "NONE" {
		if len(args) != 0 {
			return fmt.Errorf("HEALTHCHECK NONE takes no arguments")
		}
		test := strslice.StrSlice{typ}
		b.RunConfig.Healthcheck = &docker.HealthConfig{
			Test: test,
		}
	} else {
		if b.RunConfig.Healthcheck != nil {
			oldCmd := b.RunConfig.Healthcheck.Test
			if len(oldCmd) > 0 && oldCmd[0] != "NONE" {
				b.Warnings = append(b.Warnings, fmt.Sprintf("Note: overriding previous HEALTHCHECK: %v\n", oldCmd))
			}
		}

		healthcheck := docker.HealthConfig{}

		flags := flag.NewFlagSet("", flag.ContinueOnError)
		flags.String("start-period", "", "")
		flags.String("start-interval", "", "")
		flags.String("interval", "", "")
		flags.String("timeout", "", "")
		flRetries := flags.String("retries", "", "")

		if err := flags.Parse(flagArgs); err != nil {
			return err
		}

		switch typ {
		case "CMD":
			cmdSlice := handleJSONArgs(args, attributes)
			if len(cmdSlice) == 0 {
				return fmt.Errorf("Missing command after HEALTHCHECK CMD")
			}

			if !attributes["json"] {
				typ = "CMD-SHELL"
			}

			healthcheck.Test = strslice.StrSlice(append([]string{typ}, cmdSlice...))
		default:
			return fmt.Errorf("Unknown type %#v in HEALTHCHECK (try CMD)", typ)
		}

		period, err := parseOptInterval(flags.Lookup("start-period"))
		if err != nil {
			return err
		}
		healthcheck.StartPeriod = period

		interval, err := parseOptInterval(flags.Lookup("interval"))
		if err != nil {
			return err
		}
		healthcheck.Interval = interval

		startInterval, err := parseOptInterval(flags.Lookup("start-interval"))
		if err != nil {
			return err
		}
		healthcheck.StartInterval = startInterval

		timeout, err := parseOptInterval(flags.Lookup("timeout"))
		if err != nil {
			return err
		}
		healthcheck.Timeout = timeout

		if *flRetries != "" {
			retries, err := strconv.ParseInt(*flRetries, 10, 32)
			if err != nil {
				return err
			}
			if retries < 1 {
				return fmt.Errorf("--retries must be at least 1 (not %d)", retries)
			}
			healthcheck.Retries = int(retries)
		} else {
			healthcheck.Retries = 0
		}
		b.RunConfig.Healthcheck = &healthcheck
	}

	return nil
}

// ARG name[=value]
//
// Adds the variable foo to the trusted list of variables that can be passed
// to builder using the --build-arg flag for expansion/subsitution or passing to 'run'.
// Dockerfile author may optionally set a default value of this variable.
func arg(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	for _, argument := range args {
		var (
			name         string
			defaultValue string
			haveDefault  bool
		)
		arg := argument
		// 'arg' can just be a name or name-value pair. Note that this is different
		// from 'env' that handles the split of name and value at the parser level.
		// The reason for doing it differently for 'arg' is that we support just
		// defining an arg without assigning it a value (while 'env' always expects a
		// name-value pair). If possible, it will be good to harmonize the two.
		name, defaultValue, haveDefault = strings.Cut(arg, "=")

		// add the arg to allowed list of build-time args from this step on.
		b.AllowedArgs[name] = true

		// If the stage introduces one of the predefined args, add the
		// predefined value to the list of values known in this stage
		if value, defined := builtinArgDefaults[name]; defined {
			if haveDefault && (name == "TARGETPLATFORM" || name == "BUILDPLATFORM") {
				return fmt.Errorf("attempted to redefine %q: %w", name, errdefs.ErrInvalidArgument)
			}
			if b.BuiltinArgDefaults == nil {
				b.BuiltinArgDefaults = make(map[string]string)
			}
			// N.B.: we're only consulting b.BuiltinArgDefaults for
			// values that correspond to keys in
			// builtinArgDefaults, which keeps the caller from
			// using it to sneak in arbitrary ARG values
			if _, setByUser := b.UserArgs[name]; !setByUser && defined {
				if builderValue, builderDefined := b.BuiltinArgDefaults[name]; builderDefined {
					b.Args[name] = builderValue
				} else {
					b.Args[name] = value
				}
			}
			continue
		}

		// If there is still no default value, check for a default value from the heading args
		if !haveDefault {
			defaultValue, haveDefault = b.HeadingArgs[name]
		}

		// If there is a default value provided for this arg, and the user didn't supply
		// a value, then set the default value in b.Args.  Later defaults given for the
		// same arg override earlier ones.  The args passed to the builder (UserArgs) override
		// any default values of 'arg', so don't set them here as they were already set
		// in NewBuilder().
		if _, setByUser := b.UserArgs[name]; !setByUser && haveDefault {
			b.Args[name] = defaultValue
		}
	}

	return nil
}

// SHELL powershell -command
//
// Set the non-default shell to use.
func shell(b *Builder, args []string, attributes map[string]bool, flagArgs []string, original string, heredocs []buildkitparser.Heredoc) error {
	shellSlice := handleJSONArgs(args, attributes)
	switch {
	case len(shellSlice) == 0:
		// SHELL []
		return errAtLeastOneArgument("SHELL")
	case attributes["json"]:
		// SHELL ["powershell", "-command"]
		b.RunConfig.Shell = strslice.StrSlice(shellSlice)
		// b.RunConfig.Shell = strslice.StrSlice(shellSlice)
	default:
		// SHELL powershell -command - not JSON
		return errNotJSON("SHELL")
	}
	return nil
}

// checkChmodConversion makes sure that the argument to a --chmod= flag for
// COPY or ADD is an octal number
func checkChmodConversion(chmod string) error {
	_, err := strconv.ParseUint(chmod, 8, 32)
	if err != nil {
		return fmt.Errorf("Error parsing chmod %s", chmod)
	}
	return nil
}

func errAtLeastOneArgument(command string) error {
	return fmt.Errorf("%s requires at least one argument", command)
}

func errAtLeastTwoArgument(command string) error {
	return fmt.Errorf("%s requires at least two arguments", command)
}

func errExactlyOneArgument(command string) error {
	return fmt.Errorf("%s requires exactly one argument", command)
}

func errTooManyArguments(command string) error {
	return fmt.Errorf("Bad input to %s, too many arguments", command)
}

func errNotJSON(command string) error {
	return fmt.Errorf("%s requires the arguments to be in JSON form", command)
}
