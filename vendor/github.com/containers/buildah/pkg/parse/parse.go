package parse

// this package should contain functions that parse and validate
// user input and is shared either amongst buildah subcommands or
// would be useful to projects vendoring buildah

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/containerd/platforms"
	"github.com/containers/buildah/define"
	mkcwtypes "github.com/containers/buildah/internal/mkcw/types"
	internalParse "github.com/containers/buildah/internal/parse"
	"github.com/containers/buildah/internal/sbom"
	"github.com/containers/buildah/internal/tmpdir"
	"github.com/containers/buildah/pkg/sshagent"
	securejoin "github.com/cyphar/filepath-securejoin"
	units "github.com/docker/go-units"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/openshift/imagebuilder"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/common/pkg/auth"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/parse"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/unshare"
	storageTypes "go.podman.io/storage/types"
	"golang.org/x/term"
)

const (
	// SeccompDefaultPath defines the default seccomp path
	SeccompDefaultPath = config.SeccompDefaultPath
	// SeccompOverridePath if this exists it overrides the default seccomp path
	SeccompOverridePath = config.SeccompOverridePath
	// TypeBind is the type for mounting host dir
	TypeBind = "bind"
	// TypeTmpfs is the type for mounting tmpfs
	TypeTmpfs = "tmpfs"
	// TypeCache is the type for mounting a common persistent cache from host
	TypeCache = "cache"
	// mount=type=cache must create a persistent directory on host so it's available for all consecutive builds.
	// Lifecycle of following directory will be inherited from how host machine treats temporary directory
	BuildahCacheDir = "buildah-cache"
)

var errInvalidSecretSyntax = errors.New("incorrect secret flag format: should be --secret id=foo,src=bar[,env=ENV][,type=file|env]")

// RepoNamesToNamedReferences parse the raw string to Named reference
func RepoNamesToNamedReferences(destList []string) ([]reference.Named, error) {
	var result []reference.Named
	for _, dest := range destList {
		named, err := reference.ParseNormalizedNamed(dest)
		if err != nil {
			return nil, fmt.Errorf("invalid repo %q: must contain registry and repository: %w", dest, err)
		}
		if !reference.IsNameOnly(named) {
			return nil, fmt.Errorf("repository must contain neither a tag nor digest: %v", named)
		}
		result = append(result, named)
	}
	return result, nil
}

// CommonBuildOptions parses the build options from the bud cli
func CommonBuildOptions(c *cobra.Command) (*define.CommonBuildOptions, error) {
	return CommonBuildOptionsFromFlagSet(c.Flags(), c.Flag)
}

// If user selected to run with currentLabelOpts then append on the current user and role
func currentLabelOpts() ([]string, error) {
	label, err := selinux.CurrentLabel()
	if err != nil {
		return nil, err
	}
	if label == "" {
		return nil, nil
	}
	con, err := selinux.NewContext(label)
	if err != nil {
		return nil, err
	}
	return []string{
		fmt.Sprintf("label=user:%s", con["user"]),
		fmt.Sprintf("label=role:%s", con["role"]),
	}, nil
}

// CommonBuildOptionsFromFlagSet parses the build options from the bud cli
func CommonBuildOptionsFromFlagSet(flags *pflag.FlagSet, findFlagFunc func(name string) *pflag.Flag) (*define.CommonBuildOptions, error) {
	var (
		memoryLimit int64
		memorySwap  int64
		noDNS       bool
		err         error
	)

	memVal, _ := flags.GetString("memory")
	if memVal != "" {
		memoryLimit, err = units.RAMInBytes(memVal)
		if err != nil {
			return nil, fmt.Errorf("invalid value for memory: %w", err)
		}
	}

	memSwapValue, _ := flags.GetString("memory-swap")
	if memSwapValue != "" {
		if memSwapValue == "-1" {
			memorySwap = -1
		} else {
			memorySwap, err = units.RAMInBytes(memSwapValue)
			if err != nil {
				return nil, fmt.Errorf("invalid value for memory-swap: %w", err)
			}
		}
	}

	noHostname, _ := flags.GetBool("no-hostname")
	noHosts, _ := flags.GetBool("no-hosts")

	addHost, _ := flags.GetStringSlice("add-host")
	if len(addHost) > 0 {
		if noHosts {
			return nil, errors.New("--no-hosts and --add-host conflict, can not be used together")
		}
		for _, host := range addHost {
			if err := validateExtraHost(host); err != nil {
				return nil, fmt.Errorf("invalid value for add-host: %w", err)
			}
		}
	}

	noDNS = false
	dnsServers := []string{}
	if flags.Changed("dns") {
		dnsServers, _ = flags.GetStringSlice("dns")
		for _, server := range dnsServers {
			if strings.ToLower(server) == "none" {
				noDNS = true
			}
		}
		if noDNS && len(dnsServers) > 1 {
			return nil, errors.New("invalid --dns, --dns=none may not be used with any other --dns options")
		}
	}

	dnsSearch := []string{}
	if flags.Changed("dns-search") {
		dnsSearch, _ = flags.GetStringSlice("dns-search")
		if noDNS && len(dnsSearch) > 0 {
			return nil, errors.New("invalid --dns-search, --dns-search may not be used with --dns=none")
		}
	}

	dnsOptions := []string{}
	if flags.Changed("dns-option") {
		dnsOptions, _ = flags.GetStringSlice("dns-option")
		if noDNS && len(dnsOptions) > 0 {
			return nil, errors.New("invalid --dns-option, --dns-option may not be used with --dns=none")
		}
	}

	if _, err := units.FromHumanSize(findFlagFunc("shm-size").Value.String()); err != nil {
		return nil, fmt.Errorf("invalid --shm-size: %w", err)
	}
	volumes, _ := flags.GetStringArray("volume")
	cpuPeriod, _ := flags.GetUint64("cpu-period")
	cpuQuota, _ := flags.GetInt64("cpu-quota")
	cpuShares, _ := flags.GetUint64("cpu-shares")
	httpProxy, _ := flags.GetBool("http-proxy")
	var identityLabel types.OptionalBool
	if flags.Changed("identity-label") {
		b, _ := flags.GetBool("identity-label")
		identityLabel = types.NewOptionalBool(b)
	}
	omitHistory, _ := flags.GetBool("omit-history")

	ulimit := []string{}
	if flags.Changed("ulimit") {
		ulimit, _ = flags.GetStringSlice("ulimit")
	}

	secrets, _ := flags.GetStringArray("secret")
	sshsources, _ := flags.GetStringArray("ssh")
	ociHooks, _ := flags.GetStringArray("hooks-dir")

	commonOpts := &define.CommonBuildOptions{
		AddHost:       addHost,
		CPUPeriod:     cpuPeriod,
		CPUQuota:      cpuQuota,
		CPUSetCPUs:    findFlagFunc("cpuset-cpus").Value.String(),
		CPUSetMems:    findFlagFunc("cpuset-mems").Value.String(),
		CPUShares:     cpuShares,
		CgroupParent:  findFlagFunc("cgroup-parent").Value.String(),
		DNSOptions:    dnsOptions,
		DNSSearch:     dnsSearch,
		DNSServers:    dnsServers,
		HTTPProxy:     httpProxy,
		IdentityLabel: identityLabel,
		Memory:        memoryLimit,
		MemorySwap:    memorySwap,
		NoHostname:    noHostname,
		NoHosts:       noHosts,
		OmitHistory:   omitHistory,
		ShmSize:       findFlagFunc("shm-size").Value.String(),
		Ulimit:        ulimit,
		Volumes:       volumes,
		Secrets:       secrets,
		SSHSources:    sshsources,
		OCIHooksDir:   ociHooks,
	}
	securityOpts, _ := flags.GetStringArray("security-opt")
	defConfig, err := config.Default()
	if err != nil {
		return nil, fmt.Errorf("failed to get container config: %w", err)
	}
	if defConfig.Containers.EnableLabeledUsers {
		defSecurityOpts, err := currentLabelOpts()
		if err != nil {
			return nil, err
		}

		securityOpts = append(defSecurityOpts, securityOpts...)
	}
	if err := parseSecurityOpts(securityOpts, commonOpts); err != nil {
		return nil, err
	}
	return commonOpts, nil
}

// GetAdditionalBuildContext consumes raw string and returns parsed AdditionalBuildContext
func GetAdditionalBuildContext(value string) (define.AdditionalBuildContext, error) {
	ret := define.AdditionalBuildContext{IsURL: false, IsImage: false, Value: value}
	if strings.HasPrefix(value, "docker-image://") {
		ret.IsImage = true
		ret.Value = strings.TrimPrefix(value, "docker-image://")
	} else if strings.HasPrefix(value, "container-image://") {
		ret.IsImage = true
		ret.Value = strings.TrimPrefix(value, "container-image://")
	} else if strings.HasPrefix(value, "docker://") {
		ret.IsImage = true
		ret.Value = strings.TrimPrefix(value, "docker://")
	} else if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		ret.IsImage = false
		ret.IsURL = true
	} else {
		path, err := filepath.Abs(value)
		if err != nil {
			return define.AdditionalBuildContext{}, fmt.Errorf("unable to convert additional build-context %q path to absolute: %w", value, err)
		}
		ret.Value = path
	}
	return ret, nil
}

func parseSecurityOpts(securityOpts []string, commonOpts *define.CommonBuildOptions) error {
	for _, opt := range securityOpts {
		if opt == "no-new-privileges" {
			commonOpts.NoNewPrivileges = true
			continue
		}

		con := strings.SplitN(opt, "=", 2)
		if len(con) != 2 {
			return fmt.Errorf("invalid --security-opt name=value pair: %q", opt)
		}
		switch con[0] {
		case "label":
			commonOpts.LabelOpts = append(commonOpts.LabelOpts, con[1])
		case "apparmor":
			commonOpts.ApparmorProfile = con[1]
		case "seccomp":
			commonOpts.SeccompProfilePath = con[1]
		case "mask":
			commonOpts.Masks = append(commonOpts.Masks, strings.Split(con[1], ":")...)
		case "unmask":
			unmasks := strings.Split(con[1], ":")
			for _, unmask := range unmasks {
				matches, _ := filepath.Glob(unmask)
				if len(matches) > 0 {
					commonOpts.Unmasks = append(commonOpts.Unmasks, matches...)
					continue
				}
				commonOpts.Unmasks = append(commonOpts.Unmasks, unmask)
			}
		default:
			return fmt.Errorf("invalid --security-opt 2: %q", opt)
		}
	}

	if commonOpts.SeccompProfilePath == "" {
		if err := fileutils.Exists(SeccompOverridePath); err == nil {
			commonOpts.SeccompProfilePath = SeccompOverridePath
		} else {
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := fileutils.Exists(SeccompDefaultPath); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			} else {
				commonOpts.SeccompProfilePath = SeccompDefaultPath
			}
		}
	}
	return nil
}

// Split string into slice by colon. Backslash-escaped colon (i.e. "\:") will not be regarded as separator
func SplitStringWithColonEscape(str string) []string {
	return internalParse.SplitStringWithColonEscape(str)
}

// Volume parses the input of --volume
func Volume(volume string) (specs.Mount, error) {
	return internalParse.Volume(volume)
}

// Volumes validates the host and container paths passed in to the --volume flag
func Volumes(volumes []string) error {
	if len(volumes) == 0 {
		return nil
	}
	for _, volume := range volumes {
		if _, err := Volume(volume); err != nil {
			return err
		}
	}
	return nil
}

// ValidateVolumeHostDir validates a volume mount's source directory
func ValidateVolumeHostDir(hostDir string) error {
	return parse.ValidateVolumeHostDir(hostDir)
}

// ValidateVolumeCtrDir validates a volume mount's destination directory.
func ValidateVolumeCtrDir(ctrDir string) error {
	return parse.ValidateVolumeCtrDir(ctrDir)
}

// ValidateVolumeOpts validates a volume's options
func ValidateVolumeOpts(options []string) ([]string, error) {
	return parse.ValidateVolumeOpts(options)
}

// validateExtraHost validates that the specified string is a valid extrahost and returns it.
// ExtraHost is in the form of name:ip where the ip has to be a valid ip (ipv4 or ipv6).
// for add-host flag
func validateExtraHost(val string) error {
	// allow for IPv6 addresses in extra hosts by only splitting on first ":"
	arr := strings.SplitN(val, ":", 2)
	if len(arr) != 2 || len(arr[0]) == 0 {
		return fmt.Errorf("bad format for add-host: %q", val)
	}
	if arr[1] == etchosts.HostGateway {
		return nil
	}
	if _, err := validateIPAddress(arr[1]); err != nil {
		return fmt.Errorf("invalid IP address in add-host: %q", arr[1])
	}
	return nil
}

// validateIPAddress validates an Ip address.
// for dns, ip, and ip6 flags also
func validateIPAddress(val string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(val))
	if ip != nil {
		return ip.String(), nil
	}
	return "", fmt.Errorf("%s is not an ip address", val)
}

// SystemContextFromOptions returns a SystemContext populated with values
// per the input parameters provided by the caller for the use in authentication.
func SystemContextFromOptions(c *cobra.Command) (*types.SystemContext, error) {
	return SystemContextFromFlagSet(c.Flags(), c.Flag)
}

// SystemContextFromFlagSet returns a SystemContext populated with values
// per the input parameters provided by the caller for the use in authentication.
func SystemContextFromFlagSet(flags *pflag.FlagSet, findFlagFunc func(name string) *pflag.Flag) (*types.SystemContext, error) {
	certDir, err := flags.GetString("cert-dir")
	if err != nil {
		certDir = ""
	}
	ctx := &types.SystemContext{
		DockerCertPath: certDir,
	}
	tlsVerify, err := flags.GetBool("tls-verify")
	if err == nil && findFlagFunc("tls-verify").Changed {
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!tlsVerify)
		ctx.OCIInsecureSkipTLSVerify = !tlsVerify
		ctx.DockerDaemonInsecureSkipTLSVerify = !tlsVerify
	}
	insecure, err := flags.GetBool("insecure")
	if err == nil && findFlagFunc("insecure").Changed {
		if ctx.DockerInsecureSkipTLSVerify != types.OptionalBoolUndefined {
			return nil, errors.New("--insecure may not be used with --tls-verify")
		}
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(insecure)
		ctx.OCIInsecureSkipTLSVerify = insecure
		ctx.DockerDaemonInsecureSkipTLSVerify = insecure
	}
	disableCompression, err := flags.GetBool("disable-compression")
	if err == nil {
		if disableCompression {
			ctx.OCIAcceptUncompressedLayers = true
		} else {
			ctx.DirForceCompress = true
		}
	}
	creds, err := flags.GetString("creds")
	if err == nil && findFlagFunc("creds").Changed {
		var err error
		ctx.DockerAuthConfig, err = AuthConfig(creds)
		if err != nil {
			return nil, err
		}
	}
	sigPolicy, err := flags.GetString("signature-policy")
	if err == nil && findFlagFunc("signature-policy").Changed {
		ctx.SignaturePolicyPath = sigPolicy
	}
	authfile, err := flags.GetString("authfile")
	if err == nil {
		ctx.AuthFilePath = getAuthFile(authfile)
	}
	regConf, err := flags.GetString("registries-conf")
	if err == nil && findFlagFunc("registries-conf").Changed {
		ctx.SystemRegistriesConfPath = regConf
	}
	regConfDir, err := flags.GetString("registries-conf-dir")
	if err == nil && findFlagFunc("registries-conf-dir").Changed {
		ctx.RegistriesDirPath = regConfDir
	}
	shortNameAliasConf, err := flags.GetString("short-name-alias-conf")
	if err == nil && findFlagFunc("short-name-alias-conf").Changed {
		ctx.UserShortNameAliasConfPath = shortNameAliasConf
	}
	ctx.DockerRegistryUserAgent = fmt.Sprintf("Buildah/%s", define.Version)
	if findFlagFunc("os") != nil && findFlagFunc("os").Changed {
		var os string
		if os, err = flags.GetString("os"); err != nil {
			return nil, err
		}
		ctx.OSChoice = os
	}
	if findFlagFunc("arch") != nil && findFlagFunc("arch").Changed {
		var arch string
		if arch, err = flags.GetString("arch"); err != nil {
			return nil, err
		}
		ctx.ArchitectureChoice = arch
	}
	if findFlagFunc("variant") != nil && findFlagFunc("variant").Changed {
		var variant string
		if variant, err = flags.GetString("variant"); err != nil {
			return nil, err
		}
		ctx.VariantChoice = variant
	}
	if findFlagFunc("platform") != nil && findFlagFunc("platform").Changed {
		var specs []string
		if specs, err = flags.GetStringSlice("platform"); err != nil {
			return nil, err
		}
		if len(specs) == 0 || specs[0] == "" {
			return nil, fmt.Errorf("unable to parse --platform value %v", specs)
		}
		platform := specs[0]
		os, arch, variant, err := Platform(platform)
		if err != nil {
			return nil, err
		}
		if ctx.OSChoice != "" || ctx.ArchitectureChoice != "" || ctx.VariantChoice != "" {
			return nil, errors.New("invalid --platform may not be used with --os, --arch, or --variant")
		}
		ctx.OSChoice = os
		ctx.ArchitectureChoice = arch
		ctx.VariantChoice = variant
	}

	ctx.BigFilesTemporaryDir = GetTempDir()
	return ctx, nil
}

// pullPolicyWithFlags parses a string value of a pull policy, evaluating it in
// combination with "always" and "never" boolean flags.
// Allow for:
// * --pull
// * --pull=""
// * --pull=true
// * --pull=false
// * --pull=never
// * --pull=always
// * --pull=ifmissing
// * --pull=missing
// * --pull=notpresent
// * --pull=newer
// * --pull=ifnewer
// and --pull-always and --pull-never as boolean flags.
func pullPolicyWithFlags(policySpec string, always, never bool) (define.PullPolicy, error) {
	if always {
		return define.PullAlways, nil
	}
	if never {
		return define.PullNever, nil
	}
	policy := strings.ToLower(policySpec)
	switch policy {
	case "missing", "ifmissing", "notpresent":
		return define.PullIfMissing, nil
	case "true", "always":
		return define.PullAlways, nil
	case "false", "never":
		return define.PullNever, nil
	case "ifnewer", "newer":
		return define.PullIfNewer, nil
	}
	return 0, fmt.Errorf("unrecognized pull policy %q", policySpec)
}

// PullPolicyFromOptions returns a PullPolicy that reflects the combination of
// the specified "pull" and undocumented "pull-always" and "pull-never" flags.
func PullPolicyFromOptions(c *cobra.Command) (define.PullPolicy, error) {
	return PullPolicyFromFlagSet(c.Flags(), c.Flag)
}

// PullPolicyFromFlagSet returns a PullPolicy that reflects the combination of
// the specified "pull" and undocumented "pull-always" and "pull-never" flags.
func PullPolicyFromFlagSet(flags *pflag.FlagSet, findFlagFunc func(name string) *pflag.Flag) (define.PullPolicy, error) {
	pullFlagsCount := 0

	if findFlagFunc("pull").Changed {
		pullFlagsCount++
	}
	if findFlagFunc("pull-always").Changed {
		pullFlagsCount++
	}
	if findFlagFunc("pull-never").Changed {
		pullFlagsCount++
	}

	if pullFlagsCount > 1 {
		return 0, errors.New("can only set one of 'pull' or 'pull-always' or 'pull-never'")
	}

	// The --pull-never and --pull-always options will not be documented.
	pullAlwaysFlagValue, err := flags.GetBool("pull-always")
	if err != nil {
		return 0, fmt.Errorf("checking the --pull-always flag value: %w", err)
	}
	pullNeverFlagValue, err := flags.GetBool("pull-never")
	if err != nil {
		return 0, fmt.Errorf("checking the --pull-never flag value: %w", err)
	}

	// The --pull[=...] flag is the one we really care about.
	pullFlagValue := findFlagFunc("pull").Value.String()
	pullPolicy, err := pullPolicyWithFlags(pullFlagValue, pullAlwaysFlagValue, pullNeverFlagValue)
	if err != nil {
		return 0, err
	}

	logrus.Debugf("Pull Policy for pull [%v]", pullPolicy)

	return pullPolicy, nil
}

func getAuthFile(authfile string) string {
	if authfile != "" {
		absAuthfile, err := filepath.Abs(authfile)
		if err == nil {
			return absAuthfile
		}
		logrus.Warnf("ignoring passed-in auth file path, evaluating it: %v", err)
	}
	return auth.GetDefaultAuthFile()
}

// PlatformFromOptions parses the operating system (os) and architecture (arch)
// from the provided command line options.  Deprecated in favor of
// PlatformsFromOptions(), but kept here because it's part of our API.
func PlatformFromOptions(c *cobra.Command) (os, arch string, err error) {
	platforms, err := PlatformsFromOptions(c)
	if err != nil {
		return "", "", err
	}
	if len(platforms) < 1 {
		return "", "", errors.New("invalid platform syntax for --platform (use OS/ARCH[/VARIANT])")
	}
	return platforms[0].OS, platforms[0].Arch, nil
}

// PlatformsFromOptions parses the operating system (os) and architecture
// (arch) from the provided command line options.  If --platform used, it
// also returns the list of platforms that were passed in as its argument.
func PlatformsFromOptions(c *cobra.Command) (platforms []struct{ OS, Arch, Variant string }, err error) {
	var os, arch, variant string
	if c.Flag("os").Changed {
		if os, err = c.Flags().GetString("os"); err != nil {
			return nil, err
		}
	}
	if c.Flag("arch").Changed {
		if arch, err = c.Flags().GetString("arch"); err != nil {
			return nil, err
		}
	}
	if c.Flag("variant").Changed {
		if variant, err = c.Flags().GetString("variant"); err != nil {
			return nil, err
		}
	}
	platforms = []struct{ OS, Arch, Variant string }{{os, arch, variant}}
	if c.Flag("platform").Changed {
		platforms = nil
		platformSpecs, err := c.Flags().GetStringSlice("platform")
		if err != nil {
			return nil, fmt.Errorf("unable to parse platform: %w", err)
		}
		if os != "" || arch != "" || variant != "" {
			return nil, fmt.Errorf("invalid --platform may not be used with --os, --arch, or --variant")
		}
		for _, pf := range platformSpecs {
			if os, arch, variant, err = Platform(pf); err != nil {
				return nil, fmt.Errorf("unable to parse platform %q: %w", pf, err)
			}
			platforms = append(platforms, struct{ OS, Arch, Variant string }{os, arch, variant})
		}
	}
	return platforms, nil
}

// DefaultPlatform returns the standard platform for the current system
func DefaultPlatform() string {
	return platforms.DefaultString()
}

// Platform separates the platform string into os, arch and variant,
// accepting any of $arch, $os/$arch, or $os/$arch/$variant.
func Platform(platform string) (os, arch, variant string, err error) {
	platform = strings.Trim(platform, "/")
	if platform == "local" || platform == "" {
		return Platform(DefaultPlatform())
	}
	platformSpec, err := platforms.Parse(platform)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid platform syntax for --platform=%q: %w", platform, err)
	}
	return platformSpec.OS, platformSpec.Architecture, platformSpec.Variant, nil
}

func parseCreds(creds string) (string, string) {
	if creds == "" {
		return "", ""
	}
	up := strings.SplitN(creds, ":", 2)
	if len(up) == 1 {
		return up[0], ""
	}
	if up[0] == "" {
		return "", up[1]
	}
	return up[0], up[1]
}

// AuthConfig parses the creds in format [username[:password] into an auth
// config.
func AuthConfig(creds string) (*types.DockerAuthConfig, error) {
	username, password := parseCreds(creds)
	if username == "" {
		fmt.Print("Username: ")
		if _, err := fmt.Scanln(&username); err != nil {
			return nil, fmt.Errorf("reading user name: %w", err)
		}
	}
	if password == "" {
		fmt.Print("Password: ")
		termPassword, err := term.ReadPassword(0)
		if err != nil {
			return nil, fmt.Errorf("could not read password from terminal: %w", err)
		}
		password = string(termPassword)
	}

	return &types.DockerAuthConfig{
		Username: username,
		Password: password,
	}, nil
}

// GetBuildOutput is responsible for parsing custom build output argument i.e `build --output` flag.
// Takes `buildOutput` as string and returns BuildOutputOption
func GetBuildOutput(buildOutput string) (define.BuildOutputOption, error) {
	if buildOutput == "-" {
		// Feature parity with buildkit, output tar to stdout
		// Read more here: https://docs.docker.com/engine/reference/commandline/build/#custom-build-outputs
		return define.BuildOutputOption{
			Path:     "",
			IsDir:    false,
			IsStdout: true,
		}, nil
	}
	if !strings.Contains(buildOutput, ",") {
		// expect default --output <dirname>
		return define.BuildOutputOption{
			Path:     buildOutput,
			IsDir:    true,
			IsStdout: false,
		}, nil
	}
	isDir := true
	isStdout := false
	typeSelected := ""
	pathSelected := ""
	for option := range strings.SplitSeq(buildOutput, ",") {
		key, value, found := strings.Cut(option, "=")
		if !found {
			return define.BuildOutputOption{}, fmt.Errorf("invalid build output options %q, expected format key=value", buildOutput)
		}
		switch key {
		case "type":
			if typeSelected != "" {
				return define.BuildOutputOption{}, fmt.Errorf("duplicate %q not supported", key)
			}
			typeSelected = value
			switch typeSelected {
			case "local":
				isDir = true
			case "tar":
				isDir = false
			default:
				return define.BuildOutputOption{}, fmt.Errorf("invalid type %q selected for build output options %q", value, buildOutput)
			}
		case "dest":
			if pathSelected != "" {
				return define.BuildOutputOption{}, fmt.Errorf("duplicate %q not supported", key)
			}
			pathSelected = value
		default:
			return define.BuildOutputOption{}, fmt.Errorf("unrecognized key %q in build output option: %q", key, buildOutput)
		}
	}

	if typeSelected == "" || pathSelected == "" {
		return define.BuildOutputOption{}, fmt.Errorf(`invalid build output option %q, accepted keys are "type" and "dest" must be present`, buildOutput)
	}

	if pathSelected == "-" {
		if isDir {
			return define.BuildOutputOption{}, fmt.Errorf(`invalid build output option %q, "type=local" can not be used with "dest=-"`, buildOutput)
		}
	}

	return define.BuildOutputOption{Path: pathSelected, IsDir: isDir, IsStdout: isStdout}, nil
}

// TeeType parses a string value and returns a TeeType
func TeeType(teeType string) define.TeeType {
	return define.TeeType(strings.ToLower(teeType))
}

// GetConfidentialWorkloadOptions parses a confidential workload settings
// argument, which controls both whether or not we produce an image that
// expects to be run using krun, and how we handle things like encrypting
// the disk image that the container image will contain.
func GetConfidentialWorkloadOptions(arg string) (define.ConfidentialWorkloadOptions, error) {
	options := define.ConfidentialWorkloadOptions{
		TempDir: GetTempDir(),
	}
	defaults := options
	for option := range strings.SplitSeq(arg, ",") {
		var err error
		switch {
		case strings.HasPrefix(option, "type="):
			options.TeeType = TeeType(strings.TrimPrefix(option, "type="))
			switch options.TeeType {
			case define.SEV, define.SNP, mkcwtypes.SEV_NO_ES:
			default:
				return options, fmt.Errorf("parsing type= value %q: unrecognized value", options.TeeType)
			}
		case strings.HasPrefix(option, "attestation_url="), strings.HasPrefix(option, "attestation-url="):
			options.Convert = true
			options.AttestationURL = strings.TrimPrefix(option, "attestation_url=")
			if options.AttestationURL == option {
				options.AttestationURL = strings.TrimPrefix(option, "attestation-url=")
			}
		case strings.HasPrefix(option, "passphrase="):
			options.Convert = true
			options.DiskEncryptionPassphrase = strings.TrimPrefix(option, "passphrase=")
		case strings.HasPrefix(option, "workload_id="), strings.HasPrefix(option, "workload-id="):
			options.WorkloadID = strings.TrimPrefix(option, "workload_id=")
			if options.WorkloadID == option {
				options.WorkloadID = strings.TrimPrefix(option, "workload-id=")
			}
		case strings.HasPrefix(option, "cpus="):
			options.CPUs, err = strconv.Atoi(strings.TrimPrefix(option, "cpus="))
			if err != nil {
				return options, fmt.Errorf("parsing cpus= value %q: %w", strings.TrimPrefix(option, "cpus="), err)
			}
		case strings.HasPrefix(option, "memory="):
			options.Memory, err = strconv.Atoi(strings.TrimPrefix(option, "memory="))
			if err != nil {
				return options, fmt.Errorf("parsing memory= value %q: %w", strings.TrimPrefix(option, "memorys"), err)
			}
		case option == "ignore_attestation_errors", option == "ignore-attestation-errors":
			options.IgnoreAttestationErrors = true
		case strings.HasPrefix(option, "ignore_attestation_errors="), strings.HasPrefix(option, "ignore-attestation-errors="):
			val := strings.TrimPrefix(option, "ignore_attestation_errors=")
			if val == option {
				val = strings.TrimPrefix(option, "ignore-attestation-errors=")
			}
			options.IgnoreAttestationErrors = val == "true" || val == "yes" || val == "on" || val == "1"
		case strings.HasPrefix(option, "firmware-library="), strings.HasPrefix(option, "firmware_library="):
			val := strings.TrimPrefix(option, "firmware-library=")
			if val == option {
				val = strings.TrimPrefix(option, "firmware_library=")
			}
			options.FirmwareLibrary = val
		case strings.HasPrefix(option, "slop="):
			options.Slop = strings.TrimPrefix(option, "slop=")
		default:
			knownOptions := []string{"type", "attestation_url", "passphrase", "workload_id", "cpus", "memory", "firmware_library", "slop"}
			return options, fmt.Errorf("expected one or more of %q as arguments for --cw, not %q", knownOptions, option)
		}
	}
	if options != defaults && !options.Convert {
		return options, fmt.Errorf("--cw arguments missing one or more of (%q, %q)", "passphrase", "attestation_url")
	}
	return options, nil
}

// SBOMScanOptions parses the build options from the cli
func SBOMScanOptions(c *cobra.Command) (*define.SBOMScanOptions, error) {
	return SBOMScanOptionsFromFlagSet(c.Flags(), c.Flag)
}

// SBOMScanOptionsFromFlagSet parses scan settings from the cli
func SBOMScanOptionsFromFlagSet(flags *pflag.FlagSet, _ func(name string) *pflag.Flag) (*define.SBOMScanOptions, error) {
	preset, err := flags.GetString("sbom")
	if err != nil {
		return nil, fmt.Errorf("invalid value for --sbom: %w", err)
	}

	options, err := sbom.Preset(preset)
	if err != nil {
		return nil, err
	}
	if options == nil {
		return nil, fmt.Errorf("parsing --sbom flag: unrecognized preset name %q", preset)
	}
	image, err := flags.GetString("sbom-scanner-image")
	if err != nil {
		return nil, fmt.Errorf("invalid value for --sbom-scanner-image: %w", err)
	}
	commands, err := flags.GetStringArray("sbom-scanner-command")
	if err != nil {
		return nil, fmt.Errorf("invalid value for --sbom-scanner-command: %w", err)
	}
	mergeStrategy, err := flags.GetString("sbom-merge-strategy")
	if err != nil {
		return nil, fmt.Errorf("invalid value for --sbom-merge-strategy: %w", err)
	}

	if image != "" || len(commands) > 0 || mergeStrategy != "" {
		options = &define.SBOMScanOptions{
			Image:         image,
			Commands:      slices.Clone(commands),
			MergeStrategy: define.SBOMMergeStrategy(mergeStrategy),
		}
	}
	if options.ImageSBOMOutput, err = flags.GetString("sbom-image-output"); err != nil {
		return nil, fmt.Errorf("invalid value for --sbom-image-output: %w", err)
	}
	if options.SBOMOutput, err = flags.GetString("sbom-output"); err != nil {
		return nil, fmt.Errorf("invalid value for --sbom-output: %w", err)
	}
	if options.ImagePURLOutput, err = flags.GetString("sbom-image-purl-output"); err != nil {
		return nil, fmt.Errorf("invalid value for --sbom-image-purl-output: %w", err)
	}
	if options.PURLOutput, err = flags.GetString("sbom-purl-output"); err != nil {
		return nil, fmt.Errorf("invalid value for --sbom-purl-output: %w", err)
	}

	if options.Image == "" || len(options.Commands) == 0 {
		return options, fmt.Errorf("sbom configuration missing one or more of (%q or %q)", "--sbom-scanner-image", "--sbom-scanner-command")
	}
	if options.SBOMOutput == "" && options.ImageSBOMOutput == "" && options.PURLOutput == "" && options.ImagePURLOutput == "" {
		return options, fmt.Errorf("sbom configuration missing one or more of (%q, %q, %q or %q)", "--sbom-output", "--sbom-image-output", "--sbom-purl-output", "--sbom-image-purl-output")
	}
	if len(options.Commands) > 1 && options.MergeStrategy == "" {
		return options, fmt.Errorf("sbom configuration included multiple %q values but no %q value", "--sbom-scanner-command", "--sbom-merge-strategy")
	}
	switch options.MergeStrategy {
	default:
		return options, fmt.Errorf("sbom arguments included unrecognized merge strategy %q", string(options.MergeStrategy))
	case define.SBOMMergeStrategyCat, define.SBOMMergeStrategyCycloneDXByComponentNameAndVersion, define.SBOMMergeStrategySPDXByPackageNameAndVersionInfo:
		// all good here
	}
	return options, nil
}

// IDMappingOptions parses the build options related to user namespaces and ID mapping.
func IDMappingOptions(c *cobra.Command, _ define.Isolation) (usernsOptions define.NamespaceOptions, idmapOptions *define.IDMappingOptions, err error) {
	return IDMappingOptionsFromFlagSet(c.Flags(), c.PersistentFlags(), c.Flag)
}

// GetAutoOptions returns a AutoUserNsOptions with the settings to setup automatically
// a user namespace.
func GetAutoOptions(base string) (*storageTypes.AutoUserNsOptions, error) {
	parts := strings.SplitN(base, ":", 2)
	if parts[0] != "auto" {
		return nil, errors.New("wrong user namespace mode")
	}
	options := storageTypes.AutoUserNsOptions{}
	if len(parts) == 1 {
		return &options, nil
	}
	for o := range strings.SplitSeq(parts[1], ",") {
		v := strings.SplitN(o, "=", 2)
		if len(v) != 2 {
			return nil, fmt.Errorf("invalid option specified: %q", o)
		}
		switch v[0] {
		case "size":
			s, err := strconv.ParseUint(v[1], 10, 32)
			if err != nil {
				return nil, err
			}
			options.Size = uint32(s)
		case "uidmapping":
			mapping, err := storageTypes.ParseIDMapping([]string{v[1]}, nil, "", "")
			if err != nil {
				return nil, err
			}
			options.AdditionalUIDMappings = append(options.AdditionalUIDMappings, mapping.UIDMap...)
		case "gidmapping":
			mapping, err := storageTypes.ParseIDMapping(nil, []string{v[1]}, "", "")
			if err != nil {
				return nil, err
			}
			options.AdditionalGIDMappings = append(options.AdditionalGIDMappings, mapping.GIDMap...)
		default:
			return nil, fmt.Errorf("unknown option specified: %q", v[0])
		}
	}
	return &options, nil
}

// IDMappingOptionsFromFlagSet parses the build options related to user namespaces and ID mapping.
func IDMappingOptionsFromFlagSet(flags *pflag.FlagSet, persistentFlags *pflag.FlagSet, findFlagFunc func(name string) *pflag.Flag) (usernsOptions define.NamespaceOptions, idmapOptions *define.IDMappingOptions, err error) {
	isAuto := false
	autoOpts := &storageTypes.AutoUserNsOptions{}
	user := findFlagFunc("userns-uid-map-user").Value.String()
	group := findFlagFunc("userns-gid-map-group").Value.String()
	// If only the user or group was specified, use the same value for the
	// other, since we need both in order to initialize the maps using the
	// names.
	if user == "" && group != "" {
		user = group
	}
	if group == "" && user != "" {
		group = user
	}
	// Either start with empty maps or the name-based maps.
	mappings := idtools.NewIDMappingsFromMaps(nil, nil)
	if user != "" && group != "" {
		submappings, err := idtools.NewIDMappings(user, group)
		if err != nil {
			return nil, nil, err
		}
		mappings = submappings
	}
	globalOptions := persistentFlags
	// We'll parse the UID and GID mapping options the same way.
	buildIDMap := func(basemap []idtools.IDMap, option string) ([]specs.LinuxIDMapping, error) {
		outmap := make([]specs.LinuxIDMapping, 0, len(basemap))
		// Start with the name-based map entries.
		for _, m := range basemap {
			outmap = append(outmap, specs.LinuxIDMapping{
				ContainerID: uint32(m.ContainerID),
				HostID:      uint32(m.HostID),
				Size:        uint32(m.Size),
			})
		}
		// Parse the flag's value as one or more triples (if it's even
		// been set), and append them.
		var spec []string
		if globalOptions.Lookup(option) != nil && globalOptions.Lookup(option).Changed {
			spec, _ = globalOptions.GetStringSlice(option)
		}
		if findFlagFunc(option).Changed {
			spec, _ = flags.GetStringSlice(option)
		}
		idmap, err := parseIDMap(spec)
		if err != nil {
			return nil, err
		}
		for _, m := range idmap {
			outmap = append(outmap, specs.LinuxIDMapping{
				ContainerID: m[0],
				HostID:      m[1],
				Size:        m[2],
			})
		}
		return outmap, nil
	}
	uidmap, err := buildIDMap(mappings.UIDs(), "userns-uid-map")
	if err != nil {
		return nil, nil, err
	}
	gidmap, err := buildIDMap(mappings.GIDs(), "userns-gid-map")
	if err != nil {
		return nil, nil, err
	}
	// If we only have one map or the other populated at this point, then
	// use the same mapping for both, since we know that no user or group
	// name was specified, but a specific mapping was for one or the other.
	if len(uidmap) == 0 && len(gidmap) != 0 {
		uidmap = gidmap
	}
	if len(gidmap) == 0 && len(uidmap) != 0 {
		gidmap = uidmap
	}

	// By default, having mappings configured means we use a user
	// namespace.  Otherwise, we don't.
	usernsOption := define.NamespaceOption{
		Name: string(specs.UserNamespace),
		Host: len(uidmap) == 0 && len(gidmap) == 0,
	}
	// If the user specifically requested that we either use or don't use
	// user namespaces, override that default.
	if findFlagFunc("userns").Changed {
		how := findFlagFunc("userns").Value.String()
		if strings.HasPrefix(how, "auto") {
			autoOpts, err = GetAutoOptions(how)
			if err != nil {
				return nil, nil, err
			}
			isAuto = true
			usernsOption.Host = false
		} else {
			switch how {
			case "", "container", "private":
				usernsOption.Host = false
			case "host":
				usernsOption.Host = true
			default:
				how = strings.TrimPrefix(how, "ns:")
				if err := fileutils.Exists(how); err != nil {
					return nil, nil, fmt.Errorf("checking %s namespace: %w", string(specs.UserNamespace), err)
				}
				logrus.Debugf("setting %q namespace to %q", string(specs.UserNamespace), how)
				usernsOption.Path = how
			}
		}
	}
	usernsOptions = define.NamespaceOptions{usernsOption}

	// If the user requested that we use the host namespace, but also that
	// we use mappings, that's not going to work.
	if (len(uidmap) != 0 || len(gidmap) != 0) && usernsOption.Host {
		return nil, nil, fmt.Errorf("can not specify ID mappings while using host's user namespace")
	}
	return usernsOptions, &define.IDMappingOptions{
		HostUIDMapping: usernsOption.Host,
		HostGIDMapping: usernsOption.Host,
		UIDMap:         uidmap,
		GIDMap:         gidmap,
		AutoUserNs:     isAuto,
		AutoUserNsOpts: *autoOpts,
	}, nil
}

func parseIDMap(spec []string) (m [][3]uint32, err error) {
	for _, s := range spec {
		args := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
		if len(args)%3 != 0 {
			return nil, fmt.Errorf("mapping %q is not in the form containerid:hostid:size[,...]", s)
		}
		for len(args) >= 3 {
			cid, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("parsing container ID %q from mapping %q as a number: %w", args[0], s, err)
			}
			hostid, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("parsing host ID %q from mapping %q as a number: %w", args[1], s, err)
			}
			size, err := strconv.ParseUint(args[2], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("parsing %q from mapping %q as a number: %w", args[2], s, err)
			}
			m = append(m, [3]uint32{uint32(cid), uint32(hostid), uint32(size)})
			args = args[3:]
		}
	}
	return m, nil
}

// NamespaceOptions parses the build options for all namespaces except for user namespace.
func NamespaceOptions(c *cobra.Command) (namespaceOptions define.NamespaceOptions, networkPolicy define.NetworkConfigurationPolicy, err error) {
	return NamespaceOptionsFromFlagSet(c.Flags(), c.Flag)
}

// NamespaceOptionsFromFlagSet parses the build options for all namespaces except for user namespace.
func NamespaceOptionsFromFlagSet(flags *pflag.FlagSet, findFlagFunc func(name string) *pflag.Flag) (namespaceOptions define.NamespaceOptions, networkPolicy define.NetworkConfigurationPolicy, err error) {
	options := make(define.NamespaceOptions, 0, 7)
	policy := define.NetworkDefault
	for _, what := range []string{"cgroupns", string(specs.IPCNamespace), "network", string(specs.PIDNamespace), string(specs.UTSNamespace)} {
		if flags.Lookup(what) != nil && findFlagFunc(what).Changed {
			how := findFlagFunc(what).Value.String()
			switch what {
			case "cgroupns":
				what = string(specs.CgroupNamespace)
			}
			switch how {
			case "", "container", "private":
				logrus.Debugf("setting %q namespace to %q", what, "")
				policy = define.NetworkEnabled
				options.AddOrReplace(define.NamespaceOption{
					Name: what,
				})
			case "host":
				logrus.Debugf("setting %q namespace to host", what)
				policy = define.NetworkEnabled
				options.AddOrReplace(define.NamespaceOption{
					Name: what,
					Host: true,
				})
			default:
				if what == string(specs.NetworkNamespace) {
					if how == "none" {
						options.AddOrReplace(define.NamespaceOption{
							Name: what,
						})
						policy = define.NetworkDisabled
						logrus.Debugf("setting network to disabled")
						break
					}
				}
				how = strings.TrimPrefix(how, "ns:")
				// if not a path we assume it is a comma separated network list, see setupNamespaces() in run_linux.go
				if filepath.IsAbs(how) || what != string(specs.NetworkNamespace) {
					if err := fileutils.Exists(how); err != nil {
						return nil, define.NetworkDefault, fmt.Errorf("checking %s namespace: %w", what, err)
					}
				}
				policy = define.NetworkEnabled
				logrus.Debugf("setting %q namespace to %q", what, how)
				options.AddOrReplace(define.NamespaceOption{
					Name: what,
					Path: how,
				})
			}
		}
	}
	return options, policy, nil
}

func defaultIsolation() (define.Isolation, error) {
	isolation, isSet := os.LookupEnv("BUILDAH_ISOLATION")
	if isSet {
		switch strings.ToLower(isolation) {
		case "oci":
			return define.IsolationOCI, nil
		case "rootless":
			return define.IsolationOCIRootless, nil
		case "chroot":
			return define.IsolationChroot, nil
		default:
			return 0, fmt.Errorf("unrecognized $BUILDAH_ISOLATION value %q", isolation)
		}
	}
	if unshare.IsRootless() {
		return define.IsolationOCIRootless, nil
	}
	return define.IsolationDefault, nil
}

// IsolationOption parses the --isolation flag.
func IsolationOption(isolation string) (define.Isolation, error) {
	if isolation != "" {
		switch strings.ToLower(isolation) {
		case "oci", "default":
			return define.IsolationOCI, nil
		case "rootless":
			return define.IsolationOCIRootless, nil
		case "chroot":
			return define.IsolationChroot, nil
		default:
			return 0, fmt.Errorf("unrecognized isolation type %q", isolation)
		}
	}
	return defaultIsolation()
}

// Device parses device mapping string to a src, dest & permissions string
// Valid values for device look like:
//
//	'/dev/sdc"
//	'/dev/sdc:/dev/xvdc"
//	'/dev/sdc:/dev/xvdc:rwm"
//	'/dev/sdc:rm"
func Device(device string) (string, string, string, error) {
	src := ""
	dst := ""
	permissions := "rwm"
	arr := strings.Split(device, ":")
	switch len(arr) {
	case 3:
		if !isValidDeviceMode(arr[2]) {
			return "", "", "", fmt.Errorf("invalid device mode: %s", arr[2])
		}
		permissions = arr[2]
		fallthrough
	case 2:
		if isValidDeviceMode(arr[1]) {
			permissions = arr[1]
		} else {
			if len(arr[1]) == 0 || arr[1][0] != '/' {
				return "", "", "", fmt.Errorf("invalid device mode: %s", arr[1])
			}
			dst = arr[1]
		}
		fallthrough
	case 1:
		if len(arr[0]) > 0 {
			src = arr[0]
			break
		}
		fallthrough
	default:
		return "", "", "", fmt.Errorf("invalid device specification: %s", device)
	}

	if dst == "" {
		dst = src
	}
	return src, dst, permissions, nil
}

// isValidDeviceMode checks if the mode for device is valid or not.
// isValid mode is a composition of r (read), w (write), and m (mknod).
func isValidDeviceMode(mode string) bool {
	legalDeviceMode := map[rune]struct{}{
		'r': {},
		'w': {},
		'm': {},
	}
	if mode == "" {
		return false
	}
	for _, c := range mode {
		if _, has := legalDeviceMode[c]; !has {
			return false
		}
		delete(legalDeviceMode, c)
	}
	return true
}

// GetTempDir returns the path of the preferred temporary directory on the host.
func GetTempDir() string {
	return tmpdir.GetTempDir()
}

// Secrets parses the --secret flag
func Secrets(secrets []string) (map[string]define.Secret, error) {
	parsed := make(map[string]define.Secret)
	for _, secret := range secrets {
		tokens := strings.Split(secret, ",")
		var id, src, typ string
		for _, val := range tokens {
			kv := strings.SplitN(val, "=", 2)
			switch kv[0] {
			case "id":
				id = kv[1]
			case "src":
				src = kv[1]
			case "env":
				src = kv[1]
				typ = "env"
			case "type":
				if kv[1] != "file" && kv[1] != "env" {
					return nil, errors.New("invalid secret type, must be file or env")
				}
				typ = kv[1]
			default:
				return nil, errInvalidSecretSyntax
			}
		}
		if id == "" {
			return nil, errInvalidSecretSyntax
		}
		if src == "" {
			src = id
		}
		if typ == "" {
			if _, ok := os.LookupEnv(id); ok {
				typ = "env"
			} else {
				typ = "file"
			}
		}

		if typ == "file" {
			fullPath, err := filepath.Abs(src)
			if err != nil {
				return nil, fmt.Errorf("could not parse secrets: %w", err)
			}
			err = fileutils.Exists(fullPath)
			if err != nil {
				return nil, fmt.Errorf("could not parse secrets: %w", err)
			}
			src = fullPath
		}
		newSecret := define.Secret{
			ID:         id,
			Source:     src,
			SourceType: typ,
		}
		parsed[id] = newSecret
	}
	return parsed, nil
}

// SSH parses the --ssh flag
func SSH(sshSources []string) (map[string]*sshagent.Source, error) {
	parsed := make(map[string]*sshagent.Source)
	var paths []string
	for _, v := range sshSources {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) > 1 {
			paths = strings.Split(parts[1], ",")
		}

		source, err := sshagent.NewSource(paths)
		if err != nil {
			return nil, err
		}
		parsed[parts[0]] = source
	}
	return parsed, nil
}

// ContainerIgnoreFile consumes path to `dockerignore` or `containerignore`
// and returns list of files to exclude along with the path to processed ignore
// file. Deprecated since this might become internal only, please avoid relying
// on this function.
func ContainerIgnoreFile(contextDir, path string, containerFiles []string) ([]string, string, error) {
	if path != "" {
		excludes, err := imagebuilder.ParseIgnore(path)
		return excludes, path, err
	}
	// If path was not supplied give priority to `<containerfile>.containerignore` first.
	for _, containerfile := range containerFiles {
		if !filepath.IsAbs(containerfile) {
			containerfile = filepath.Join(contextDir, containerfile)
		}
		containerfileIgnore := ""
		if err := fileutils.Exists(containerfile + ".containerignore"); err == nil {
			containerfileIgnore = containerfile + ".containerignore"
		}
		if err := fileutils.Exists(containerfile + ".dockerignore"); err == nil {
			containerfileIgnore = containerfile + ".dockerignore"
		}
		if containerfileIgnore != "" {
			excludes, err := imagebuilder.ParseIgnore(containerfileIgnore)
			return excludes, containerfileIgnore, err
		}
	}
	path, symlinkErr := securejoin.SecureJoin(contextDir, ".containerignore")
	if symlinkErr != nil {
		return nil, "", symlinkErr
	}
	excludes, err := imagebuilder.ParseIgnore(path)
	if errors.Is(err, os.ErrNotExist) {
		path, symlinkErr = securejoin.SecureJoin(contextDir, ".dockerignore")
		if symlinkErr != nil {
			return nil, "", symlinkErr
		}
		excludes, err = imagebuilder.ParseIgnore(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return excludes, "", nil
	}
	return excludes, path, err
}
