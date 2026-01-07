package images

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/containers/buildah/define"
	"github.com/containers/podman/v5/internal/remote_build_helpers"
	ldefine "github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/auth"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-units"
	"github.com/hashicorp/go-multierror"
	jsoniter "github.com/json-iterator/go"
	gzip "github.com/klauspost/pgzip"
	"github.com/sirupsen/logrus"
	imageTypes "go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/regexp"
)

type devino struct {
	Dev uint64
	Ino uint64
}

var iidRegex = regexp.Delayed(`^[0-9a-f]{12}`)

type BuildResponse struct {
	Stream string                 `json:"stream,omitempty"`
	Error  *jsonmessage.JSONError `json:"errorDetail,omitempty"`
	// NOTE: `error` is being deprecated check https://github.com/moby/moby/blob/master/pkg/jsonmessage/jsonmessage.go#L148
	ErrorMessage string          `json:"error,omitempty"` // deprecate this slowly
	Aux          json.RawMessage `json:"aux,omitempty"`
}

// BuildFilePaths contains the file paths and exclusion patterns for the build context.
type BuildFilePaths struct {
	tarContent        []string
	newContainerFiles []string // dockerfile paths, relative to context dir, ToSlash()ed
	dontexcludes      []string
	excludes          []string
}

// RequestParts contains the components of an HTTP request for the build API.
type RequestParts struct {
	Headers http.Header
	Params  url.Values
	Body    io.ReadCloser
}

// Modify the build contexts that uses a local windows path. The windows path is
// converted into the corresping guest path in the default Windows machine
// (e.g. C:\test ==> /mnt/c/test).
func convertAdditionalBuildContexts(additionalBuildContexts map[string]*define.AdditionalBuildContext) {
	for _, context := range additionalBuildContexts {
		if !context.IsImage && !context.IsURL {
			path, err := specgen.ConvertWinMountPath(context.Value)
			// It's not worth failing if the path can't be converted
			if err == nil {
				context.Value = path
			}
		}
	}
}

// convertVolumeSrcPath converts windows paths in the HOST-DIR part of a volume
// into the corresponding path in the default Windows machine.
// (e.g. C:\test:/src/docs ==> /mnt/c/test:/src/docs).
// If any error occurs while parsing the volume string, the original volume
// string is returned.
func convertVolumeSrcPath(volume string) string {
	splitVol := specgen.SplitVolumeString(volume)
	if len(splitVol) < 2 || len(splitVol) > 3 {
		return volume
	}
	convertedSrcPath, err := specgen.ConvertWinMountPath(splitVol[0])
	if err != nil {
		return volume
	}
	if len(splitVol) == 2 {
		return convertedSrcPath + ":" + splitVol[1]
	} else {
		return convertedSrcPath + ":" + splitVol[1] + ":" + splitVol[2]
	}
}

// isSupportedVersion checks if the server version is greater than or equal to the specified minimum version.
// It extracts version numbers from the server version string, removing any suffixes like -dev or -rc,
// and compares them using semantic versioning.
func isSupportedVersion(ctx context.Context, minVersion string) (bool, error) {
	serverVersion := bindings.ServiceVersion(ctx)

	// Extract just the version numbers (remove -dev, -rc, etc)
	versionStr := serverVersion.String()
	if idx := strings.Index(versionStr, "-"); idx > 0 {
		versionStr = versionStr[:idx]
	}

	serverVer, err := semver.ParseTolerant(versionStr)
	if err != nil {
		return false, fmt.Errorf("parsing server version %q: %w", serverVersion, err)
	}

	minMultipartVersion, _ := semver.ParseTolerant(minVersion)

	return serverVer.GTE(minMultipartVersion), nil
}

// prepareParams converts BuildOptions into URL parameters for the build API request.
// It handles various build options including capabilities, annotations, CPU settings,
// devices, labels, platforms, volumes, and other build configuration parameters.
func prepareParams(options types.BuildOptions) (url.Values, error) {
	params := url.Values{}

	if caps := options.AddCapabilities; len(caps) > 0 {
		c, err := jsoniter.MarshalToString(caps)
		if err != nil {
			return nil, err
		}
		params.Add("addcaps", c)
	}

	if annotations := options.Annotations; len(annotations) > 0 {
		l, err := jsoniter.MarshalToString(annotations)
		if err != nil {
			return nil, err
		}
		params.Set("annotations", l)
	}

	if cppflags := options.CPPFlags; len(cppflags) > 0 {
		l, err := jsoniter.MarshalToString(cppflags)
		if err != nil {
			return nil, err
		}
		params.Set("cppflags", l)
	}

	if options.AllPlatforms {
		params.Add("allplatforms", "1")
	}

	params.Add("t", options.Output)
	for _, tag := range options.AdditionalTags {
		params.Add("t", tag)
	}

	if options.IDMappingOptions != nil {
		idmappingsOptions, err := jsoniter.Marshal(options.IDMappingOptions)
		if err != nil {
			return nil, err
		}
		params.Set("idmappingoptions", string(idmappingsOptions))
	}
	if buildArgs := options.Args; len(buildArgs) > 0 {
		bArgs, err := jsoniter.MarshalToString(buildArgs)
		if err != nil {
			return nil, err
		}
		params.Set("buildargs", bArgs)
	}
	if excludes := options.Excludes; len(excludes) > 0 {
		bArgs, err := jsoniter.MarshalToString(excludes)
		if err != nil {
			return nil, err
		}
		params.Set("excludes", bArgs)
	}
	if cpuPeriod := options.CommonBuildOpts.CPUPeriod; cpuPeriod > 0 {
		params.Set("cpuperiod", strconv.Itoa(int(cpuPeriod)))
	}
	if cpuQuota := options.CommonBuildOpts.CPUQuota; cpuQuota > 0 {
		params.Set("cpuquota", strconv.Itoa(int(cpuQuota)))
	}
	if cpuSetCpus := options.CommonBuildOpts.CPUSetCPUs; len(cpuSetCpus) > 0 {
		params.Set("cpusetcpus", cpuSetCpus)
	}
	if cpuSetMems := options.CommonBuildOpts.CPUSetMems; len(cpuSetMems) > 0 {
		params.Set("cpusetmems", cpuSetMems)
	}
	if cpuShares := options.CommonBuildOpts.CPUShares; cpuShares > 0 {
		params.Set("cpushares", strconv.Itoa(int(cpuShares)))
	}
	if len(options.CommonBuildOpts.CgroupParent) > 0 {
		params.Set("cgroupparent", options.CommonBuildOpts.CgroupParent)
	}

	params.Set("networkmode", strconv.Itoa(int(options.ConfigureNetwork)))
	params.Set("outputformat", options.OutputFormat)

	if devices := options.Devices; len(devices) > 0 {
		d, err := jsoniter.MarshalToString(devices)
		if err != nil {
			return nil, err
		}
		params.Add("devices", d)
	}

	if dnsservers := options.CommonBuildOpts.DNSServers; len(dnsservers) > 0 {
		c, err := jsoniter.MarshalToString(dnsservers)
		if err != nil {
			return nil, err
		}
		params.Add("dnsservers", c)
	}
	if dnsoptions := options.CommonBuildOpts.DNSOptions; len(dnsoptions) > 0 {
		c, err := jsoniter.MarshalToString(dnsoptions)
		if err != nil {
			return nil, err
		}
		params.Add("dnsoptions", c)
	}
	if dnssearch := options.CommonBuildOpts.DNSSearch; len(dnssearch) > 0 {
		c, err := jsoniter.MarshalToString(dnssearch)
		if err != nil {
			return nil, err
		}
		params.Add("dnssearch", c)
	}

	if caps := options.DropCapabilities; len(caps) > 0 {
		c, err := jsoniter.MarshalToString(caps)
		if err != nil {
			return nil, err
		}
		params.Add("dropcaps", c)
	}

	if options.ForceRmIntermediateCtrs {
		params.Set("forcerm", "1")
	}
	if options.RemoveIntermediateCtrs {
		params.Set("rm", "1")
	} else {
		params.Set("rm", "0")
	}
	if options.CommonBuildOpts.OmitHistory {
		params.Set("omithistory", "1")
	} else {
		params.Set("omithistory", "0")
	}
	if len(options.From) > 0 {
		params.Set("from", options.From)
	}
	if options.IgnoreUnrecognizedInstructions {
		params.Set("ignore", "1")
	}
	switch options.CreatedAnnotation {
	case imageTypes.OptionalBoolFalse:
		params.Set("createdannotation", "0")
	case imageTypes.OptionalBoolTrue:
		params.Set("createdannotation", "1")
	}
	switch options.InheritLabels {
	case imageTypes.OptionalBoolFalse:
		params.Set("inheritlabels", "0")
	case imageTypes.OptionalBoolTrue:
		params.Set("inheritlabels", "1")
	}

	if options.InheritAnnotations == imageTypes.OptionalBoolFalse {
		params.Set("inheritannotations", "0")
	} else {
		params.Set("inheritannotations", "1")
	}

	params.Set("isolation", strconv.Itoa(int(options.Isolation)))
	if options.CommonBuildOpts.HTTPProxy {
		params.Set("httpproxy", "1")
	}
	if options.Jobs != nil {
		params.Set("jobs", strconv.FormatUint(uint64(*options.Jobs), 10))
	}
	if labels := options.Labels; len(labels) > 0 {
		l, err := jsoniter.MarshalToString(labels)
		if err != nil {
			return nil, err
		}
		params.Set("labels", l)
	}

	if opt := options.CommonBuildOpts.LabelOpts; len(opt) > 0 {
		o, err := jsoniter.MarshalToString(opt)
		if err != nil {
			return nil, err
		}
		params.Set("labelopts", o)
	}

	if len(options.CommonBuildOpts.SeccompProfilePath) > 0 {
		params.Set("seccomp", options.CommonBuildOpts.SeccompProfilePath)
	}

	if len(options.CommonBuildOpts.ApparmorProfile) > 0 {
		params.Set("apparmor", options.CommonBuildOpts.ApparmorProfile)
	}

	for _, layerLabel := range options.LayerLabels {
		params.Add("layerLabel", layerLabel)
	}
	if options.Layers {
		params.Set("layers", "1")
	}
	if options.LogRusage {
		params.Set("rusage", "1")
	}
	if len(options.RusageLogFile) > 0 {
		params.Set("rusagelogfile", options.RusageLogFile)
	}

	params.Set("retry", strconv.Itoa(options.MaxPullPushRetries))
	params.Set("retry-delay", options.PullPushRetryDelay.String())

	if len(options.Manifest) > 0 {
		params.Set("manifest", options.Manifest)
	}
	if options.CacheFrom != nil {
		cacheFrom := []string{}
		for _, cacheSrc := range options.CacheFrom {
			cacheFrom = append(cacheFrom, cacheSrc.String())
		}
		cacheFromJSON, err := jsoniter.MarshalToString(cacheFrom)
		if err != nil {
			return nil, err
		}
		params.Set("cachefrom", cacheFromJSON)
	}

	switch options.SkipUnusedStages {
	case imageTypes.OptionalBoolTrue:
		params.Set("skipunusedstages", "1")
	case imageTypes.OptionalBoolFalse:
		params.Set("skipunusedstages", "0")
	}

	if options.CacheTo != nil {
		cacheTo := []string{}
		for _, cacheSrc := range options.CacheTo {
			cacheTo = append(cacheTo, cacheSrc.String())
		}
		cacheToJSON, err := jsoniter.MarshalToString(cacheTo)
		if err != nil {
			return nil, err
		}
		params.Set("cacheto", cacheToJSON)
	}
	if int64(options.CacheTTL) != 0 {
		params.Set("cachettl", options.CacheTTL.String())
	}
	if memSwap := options.CommonBuildOpts.MemorySwap; memSwap > 0 {
		params.Set("memswap", strconv.Itoa(int(memSwap)))
	}
	if mem := options.CommonBuildOpts.Memory; mem > 0 {
		params.Set("memory", strconv.Itoa(int(mem)))
	}
	switch options.CompatVolumes {
	case imageTypes.OptionalBoolTrue:
		params.Set("compatvolumes", "1")
	case imageTypes.OptionalBoolFalse:
		params.Set("compatvolumes", "0")
	}
	if options.NoCache {
		params.Set("nocache", "1")
	}
	if options.CommonBuildOpts.NoHosts {
		params.Set("nohosts", "1")
	}
	if t := options.Output; len(t) > 0 {
		params.Set("output", t)
	}
	if t := options.OSVersion; len(t) > 0 {
		params.Set("osversion", t)
	}
	for _, t := range options.OSFeatures {
		params.Set("osfeature", t)
	}
	var platform string
	if len(options.OS) > 0 {
		platform = options.OS
	}
	if len(options.Architecture) > 0 {
		if len(platform) == 0 {
			platform = "linux"
		}
		platform += "/" + options.Architecture
	} else if len(platform) > 0 {
		platform += "/" + runtime.GOARCH
	}
	if len(platform) > 0 {
		params.Set("platform", platform)
	}
	if len(options.Platforms) > 0 {
		params.Del("platform")
		for _, platformSpec := range options.Platforms {
			// podman-cli will send empty struct, in such
			// case don't add platform to param and let the
			// build backend decide the default platform.
			if platformSpec.OS == "" && platformSpec.Arch == "" && platformSpec.Variant == "" {
				continue
			}
			platform = platformSpec.OS + "/" + platformSpec.Arch
			if platformSpec.Variant != "" {
				platform += "/" + platformSpec.Variant
			}
			params.Add("platform", platform)
		}
	}

	for _, volume := range options.CommonBuildOpts.Volumes {
		params.Add("volume", convertVolumeSrcPath(volume))
	}

	for _, group := range options.GroupAdd {
		params.Add("groupadd", group)
	}

	params.Set("pullpolicy", options.PullPolicy.String())

	switch options.CommonBuildOpts.IdentityLabel {
	case imageTypes.OptionalBoolTrue:
		params.Set("identitylabel", "1")
	case imageTypes.OptionalBoolFalse:
		params.Set("identitylabel", "0")
	}
	if options.Quiet {
		params.Set("q", "1")
	}
	if options.RemoveIntermediateCtrs {
		params.Set("rm", "1")
	}
	if len(options.Target) > 0 {
		params.Set("target", options.Target)
	}

	if hosts := options.CommonBuildOpts.AddHost; len(hosts) > 0 {
		h, err := jsoniter.MarshalToString(hosts)
		if err != nil {
			return nil, err
		}
		params.Set("extrahosts", h)
	}
	if nsoptions := options.NamespaceOptions; len(nsoptions) > 0 {
		ns, err := jsoniter.MarshalToString(nsoptions)
		if err != nil {
			return nil, err
		}
		params.Set("nsoptions", ns)
	}
	if shmSize := options.CommonBuildOpts.ShmSize; len(shmSize) > 0 {
		shmBytes, err := units.RAMInBytes(shmSize)
		if err != nil {
			return nil, err
		}
		params.Set("shmsize", strconv.Itoa(int(shmBytes)))
	}
	if options.Squash {
		params.Set("squash", "1")
	}

	if options.SourceDateEpoch != nil {
		t := options.SourceDateEpoch
		params.Set("sourcedateepoch", strconv.FormatInt(t.Unix(), 10))
	}
	if options.RewriteTimestamp {
		params.Set("rewritetimestamp", "1")
	} else {
		params.Set("rewritetimestamp", "0")
	}
	if options.Timestamp != nil {
		t := options.Timestamp
		params.Set("timestamp", strconv.FormatInt(t.Unix(), 10))
	}

	if len(options.CommonBuildOpts.Ulimit) > 0 {
		ulimitsJSON, err := json.Marshal(options.CommonBuildOpts.Ulimit)
		if err != nil {
			return nil, err
		}
		params.Set("ulimits", string(ulimitsJSON))
	}

	for _, env := range options.Envs {
		params.Add("setenv", env)
	}

	for _, uenv := range options.UnsetEnvs {
		params.Add("unsetenv", uenv)
	}

	for _, ulabel := range options.UnsetLabels {
		params.Add("unsetlabel", ulabel)
	}

	for _, uannotation := range options.UnsetAnnotations {
		params.Add("unsetannotation", uannotation)
	}

	return params, nil
}

// prepareAuthHeaders sets up authentication headers for the build request.
// It handles Docker authentication configuration and TLS verification settings
// from the system context.
func prepareAuthHeaders(options types.BuildOptions, requestParts *RequestParts) (*RequestParts, error) {
	var err error

	if options.SystemContext == nil {
		return requestParts, err
	}

	if options.SystemContext.DockerAuthConfig != nil {
		requestParts.Headers, err = auth.MakeXRegistryAuthHeader(options.SystemContext, options.SystemContext.DockerAuthConfig.Username, options.SystemContext.DockerAuthConfig.Password)
	} else {
		requestParts.Headers, err = auth.MakeXRegistryConfigHeader(options.SystemContext, "", "")
	}
	if options.SystemContext.DockerInsecureSkipTLSVerify == imageTypes.OptionalBoolTrue {
		requestParts.Params.Set("tlsVerify", "false")
	}

	return requestParts, err
}

// prepareContainerFiles processes container files (Dockerfiles/Containerfiles) for the build.
// It handles URLs, stdin input, symlinks, and determines which files need to be included
// in the tar archive versus which are already in the context directory.
// The stdinDestination parameter specifies where to save stdin content when processing /dev/stdin.
// WARNING: Caller must ensure tempManager.Cleanup() is called to remove any temporary files created.
func prepareContainerFiles(containerFiles []string, contextDir string, stdinDestination string, tempManager *remote_build_helpers.TempFileManager) (*BuildFilePaths, error) {
	out := BuildFilePaths{
		tarContent:        []string{contextDir},
		newContainerFiles: []string{}, // dockerfile paths, relative to context dir, ToSlash()ed
		dontexcludes:      []string{"!Dockerfile", "!Containerfile", "!.dockerignore", "!.containerignore"},
		excludes:          []string{},
	}

	for _, c := range containerFiles {
		// Do not add path to containerfile if it is a URL
		if strings.HasPrefix(c, "http://") || strings.HasPrefix(c, "https://") {
			out.newContainerFiles = append(out.newContainerFiles, c)
			continue
		}
		if c == "/dev/stdin" {
			stdinFile, err := tempManager.CreateTempFileFromReader(stdinDestination, "podman-build-stdin-*", os.Stdin)
			if err != nil {
				return nil, fmt.Errorf("processing stdin: %w", err)
			}
			c = stdinFile
		}
		c = filepath.Clean(c)
		cfDir := filepath.Dir(c)
		if absDir, err := filepath.EvalSymlinks(cfDir); err == nil {
			name := filepath.ToSlash(strings.TrimPrefix(c, cfDir+string(filepath.Separator)))
			c = filepath.Join(absDir, name)
		}

		containerfile, err := filepath.Abs(c)
		if err != nil {
			logrus.Errorf("Cannot find absolute path of %v: %v", c, err)
			return nil, err
		}

		// Check if Containerfile is in the context directory, if so truncate the context directory off path
		// Do NOT add to tarfile
		if after, ok := strings.CutPrefix(containerfile, contextDir+string(filepath.Separator)); ok {
			containerfile = after
			out.dontexcludes = append(out.dontexcludes, "!"+containerfile)
			out.dontexcludes = append(out.dontexcludes, "!"+containerfile+".dockerignore")
			out.dontexcludes = append(out.dontexcludes, "!"+containerfile+".containerignore")
		} else {
			// If Containerfile does not exist, assume it is in context directory and do Not add to tarfile
			if err := fileutils.Lexists(containerfile); err != nil {
				if !os.IsNotExist(err) {
					return nil, err
				}
				containerfile = c
				out.dontexcludes = append(out.dontexcludes, "!"+containerfile)
				out.dontexcludes = append(out.dontexcludes, "!"+containerfile+".dockerignore")
				out.dontexcludes = append(out.dontexcludes, "!"+containerfile+".containerignore")
			} else {
				// If Containerfile does exist and not in the context directory, add it to the tarfile
				out.tarContent = append(out.tarContent, containerfile)
			}
		}
		out.newContainerFiles = append(out.newContainerFiles, filepath.ToSlash(containerfile))
	}

	return &out, nil
}

// prepareSecrets processes build secrets by creating temporary files for them.
// It moves secrets to the context directory and modifies the secret configuration
// to use relative paths suitable for remote builds.
// WARNING: Caller must ensure tempManager.Cleanup() is called to remove any temporary files created.
func prepareSecrets(secrets []string, contextDir string, tempManager *remote_build_helpers.TempFileManager) ([]string, []string, error) {
	if len(secrets) == 0 {
		return nil, nil, nil
	}

	secretsForRemote := []string{}
	tarContent := []string{}

	for _, secret := range secrets {
		secretOpt := strings.Split(secret, ",")
		modifiedOpt := []string{}
		for _, token := range secretOpt {
			opt, val, hasVal := strings.Cut(token, "=")
			if hasVal {
				if opt == "src" {
					// read specified secret into a tmp file
					// move tmp file to tar and change secret source to relative tmp file
					tmpSecretFilePath, err := tempManager.CreateTempSecret(val, contextDir)
					if err != nil {
						return nil, nil, err
					}

					// add tmp file to context dir
					tarContent = append(tarContent, tmpSecretFilePath)

					modifiedSrc := fmt.Sprintf("src=%s", filepath.Base(tmpSecretFilePath))
					modifiedOpt = append(modifiedOpt, modifiedSrc)
				} else {
					modifiedOpt = append(modifiedOpt, token)
				}
			}
		}
		secretsForRemote = append(secretsForRemote, strings.Join(modifiedOpt, ","))
	}

	return secretsForRemote, tarContent, nil
}

// prepareRemoteRequestBody creates the request body for the build API call.
// It handles both simple tar archives and multipart form data for builds with
// additional build contexts, supporting URLs, images, and local directories.
// WARNING: Caller must close request body.
func prepareRemoteRequestBody(ctx context.Context, requestParts *RequestParts, buildFilePaths *BuildFilePaths, options types.BuildOptions) (*RequestParts, error) {
	tarfile, err := nTar(append(buildFilePaths.excludes, buildFilePaths.dontexcludes...), buildFilePaths.tarContent...)
	if err != nil {
		logrus.Errorf("Cannot tar container entries %v error: %v", buildFilePaths.tarContent, err)
		return nil, err
	}

	var contentType string

	// If there are additional build contexts, we need to handle them based on the server version
	// podman version >= 5.6.0 supports multipart/form-data for additional build contexts that
	// are local directories or archives. URLs and images are still sent as query parameters.
	isSupported, err := isSupportedVersion(ctx, "5.6.0")
	if err != nil {
		return nil, err
	}

	if len(options.SBOMScanOptions) > 0 {
		for _, sbomScanOpts := range options.SBOMScanOptions {
			if sbomScanOpts.SBOMOutput != "" {
				requestParts.Params.Set("sbom-output", sbomScanOpts.SBOMOutput)
			}

			if sbomScanOpts.PURLOutput != "" {
				requestParts.Params.Set("sbom-purl-output", sbomScanOpts.PURLOutput)
			}

			if sbomScanOpts.ImageSBOMOutput != "" {
				requestParts.Params.Set("sbom-image-output", sbomScanOpts.ImageSBOMOutput)
			}

			if sbomScanOpts.ImagePURLOutput != "" {
				requestParts.Params.Set("sbom-image-purl-output", sbomScanOpts.ImagePURLOutput)
			}

			if sbomScanOpts.Image != "" {
				requestParts.Params.Set("sbom-scanner-image", sbomScanOpts.Image)
			}

			if commands := sbomScanOpts.Commands; len(commands) > 0 {
				c, err := jsoniter.MarshalToString(commands)
				if err != nil {
					return nil, err
				}
				requestParts.Params.Add("sbom-scanner-command", c)
			}

			if sbomScanOpts.MergeStrategy != "" {
				requestParts.Params.Set("sbom-merge-strategy", string(sbomScanOpts.MergeStrategy))
			}
		}
	}

	if len(options.AdditionalBuildContexts) == 0 {
		requestParts.Body = tarfile
		logrus.Debugf("Using main build context: %q", options.ContextDirectory)
		return requestParts, nil
	}

	if !isSupported {
		convertAdditionalBuildContexts(options.AdditionalBuildContexts)
		additionalBuildContextMap, err := jsoniter.Marshal(options.AdditionalBuildContexts)
		if err != nil {
			return nil, err
		}
		requestParts.Params.Set("additionalbuildcontexts", string(additionalBuildContextMap))

		requestParts.Body = tarfile
		logrus.Debugf("Using main build context: %q", options.ContextDirectory)
		return requestParts, nil
	}

	imageContexts := make(map[string]string)
	urlContexts := make(map[string]string)
	localContexts := make(map[string]*define.AdditionalBuildContext)

	for name, context := range options.AdditionalBuildContexts {
		switch {
		case context.IsImage:
			imageContexts[name] = context.Value
		case context.IsURL:
			urlContexts[name] = context.Value
		default:
			localContexts[name] = context
		}
	}

	logrus.Debugf("URL Contexts: %v", urlContexts)
	for name, url := range urlContexts {
		requestParts.Params.Add("additionalbuildcontexts", fmt.Sprintf("%s=url:%s", name, url))
	}

	logrus.Debugf("Image Contexts: %v", imageContexts)
	for name, imageRef := range imageContexts {
		requestParts.Params.Add("additionalbuildcontexts", fmt.Sprintf("%s=image:%s", name, imageRef))
	}

	if len(localContexts) == 0 {
		requestParts.Body = tarfile
		logrus.Debugf("Using main build context: %q", options.ContextDirectory)
		return requestParts, nil
	}
	// Multipart request structure:
	// - "MainContext": The main build context as a tar file
	// - "build-context-<name>": Each additional local context as a tar file
	logrus.Debugf("Using additional local build contexts: %v", localContexts)
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	contentType = writer.FormDataContentType()
	requestParts.Body = pr

	if requestParts.Headers == nil {
		requestParts.Headers = make(http.Header)
	}
	requestParts.Headers.Set("Content-Type", contentType)

	go func() {
		defer pw.Close()
		defer writer.Close()

		mainContext, err := writer.CreateFormFile("MainContext", "MainContext.tar")
		if err != nil {
			pw.CloseWithError(fmt.Errorf("creating form file for main context: %w", err))
			return
		}

		if _, err := io.Copy(mainContext, tarfile); err != nil {
			pw.CloseWithError(fmt.Errorf("copying main context: %w", err))
			return
		}

		defer func() {
			if err := tarfile.Close(); err != nil {
				logrus.Errorf("failed to close context tarfile: %v\n", err)
			}
		}()

		for name, context := range localContexts {
			logrus.Debugf("Processing additional local context: %s", name)
			part, err := writer.CreateFormFile(fmt.Sprintf("build-context-%s", name), name)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("creating form file for context %q: %w", name, err))
				return
			}

			// Context is already a tar
			if archive.IsArchivePath(context.Value) {
				file, err := os.Open(context.Value)
				if err != nil {
					pw.CloseWithError(fmt.Errorf("opening archive %q: %w", name, err))
					return
				}
				if _, err := io.Copy(part, file); err != nil {
					file.Close()
					pw.CloseWithError(fmt.Errorf("copying context %q: %w", name, err))
					return
				}
				file.Close()
			} else {
				tarContent, err := nTar(nil, context.Value)
				if err != nil {
					pw.CloseWithError(fmt.Errorf("creating tar content %q: %w", name, err))
					return
				}
				if _, err = io.Copy(part, tarContent); err != nil {
					pw.CloseWithError(fmt.Errorf("copying tar content %q: %w", name, err))
					return
				}
				if err := tarContent.Close(); err != nil {
					logrus.Errorf("Error closing tar content for context %q: %v\n", name, err)
				}
			}
		}
	}()
	logrus.Debugf("Multipart body is created with content type: %s", contentType)

	return requestParts, nil
}

// executeBuildRequest sends the build request to the API endpoint and returns the response.
// It handles the HTTP request creation and error checking for the build operation.
// WARNING: Caller must close the response body.
func executeBuildRequest(ctx context.Context, endpoint string, requestParts *RequestParts) (*bindings.APIResponse, error) {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	response, err := conn.DoRequest(ctx, requestParts.Body, http.MethodPost, endpoint, requestParts.Params, requestParts.Headers)
	if err != nil {
		return nil, err
	}

	if !response.IsSuccess() {
		return nil, response.Process(err)
	}

	return response, nil
}

// processBuildResponse processes the streaming build response from the API.
// It reads the JSON stream, extracts build output and errors, writes to stdout,
// and returns a build report with the final image ID.
func processBuildResponse(response *bindings.APIResponse, stdout io.Writer, saveFormat string) (*types.BuildReport, error) {
	body := response.Body.(io.Reader)
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		if v, found := os.LookupEnv("PODMAN_RETAIN_BUILD_ARTIFACT"); found {
			if keep, _ := strconv.ParseBool(v); keep {
				t, _ := os.CreateTemp("", "build_*_client")
				defer t.Close()
				body = io.TeeReader(response.Body, t)
			}
		}
	}

	dec := json.NewDecoder(body)

	var id string
	for {
		var s BuildResponse
		select {
		// FIXME(vrothberg): it seems we always hit the EOF case below,
		// even when the server quit but it seems desirable to
		// distinguish a proper build from a transient EOF.
		case <-response.Request.Context().Done():
			return &types.BuildReport{ID: id, SaveFormat: saveFormat}, nil
		default:
			// non-blocking select
		}

		if err := dec.Decode(&s); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("server probably quit: %w", err)
			}
			// EOF means the stream is over in which case we need
			// to have read the id.
			if errors.Is(err, io.EOF) && id != "" {
				break
			}
			return &types.BuildReport{ID: id, SaveFormat: saveFormat}, fmt.Errorf("decoding stream: %w", err)
		}

		switch {
		case s.Stream != "":
			raw := []byte(s.Stream)
			stdout.Write(raw)
			if iidRegex.Match(raw) {
				id = strings.TrimSuffix(s.Stream, "\n")
			}
		case s.Error != nil:
			// If there's an error, return directly.  The stream
			// will be closed on return.
			return &types.BuildReport{ID: id, SaveFormat: saveFormat}, errors.New(s.Error.Message)
		default:
			return &types.BuildReport{ID: id, SaveFormat: saveFormat}, errors.New("failed to parse build results stream, unexpected input")
		}
	}
	return &types.BuildReport{ID: id, SaveFormat: saveFormat}, nil
}

// prepareLocalRequestBody prepares HTTP request parameters for local build API calls.
// It sets up local context directory and additional build contexts using already translated paths.
func prepareLocalRequestBody(_ context.Context, requestParts *RequestParts, _ *BuildFilePaths, options types.BuildOptions) (*RequestParts, error) {
	requestParts.Params.Set("localcontextdir", options.ContextDirectory)

	for name, context := range options.AdditionalBuildContexts {
		switch {
		case context.IsImage:
			requestParts.Params.Add("additionalbuildcontexts", fmt.Sprintf("%s=image:%s", name, context.Value))
		case context.IsURL:
			requestParts.Params.Add("additionalbuildcontexts", fmt.Sprintf("%s=url:%s", name, context.Value))
		default:
			requestParts.Params.Add("additionalbuildcontexts", fmt.Sprintf("%s=localpath:%s", name, context.Value))
		}
	}
	return requestParts, nil
}

// BuildFromServerContext performs a container image build using the local build API to build image with files present on server.
//
// Unlike the standard Build function, this uses existing files on the remote server's filesystem
// rather than uploading build contexts. The containerFiles and options parameters should contain
// already translated paths pointing to files on the remote server, making it suitable for scenarios
// where build contexts already exist on the server (e.g., shared filesystems, mounted volumes).
//
// The context directory and containerFiles paths must be accessible on the remote server.
// Missing paths will result in build errors.
//
// Returns a BuildReport containing the final image ID and save format.
func BuildFromServerContext(ctx context.Context, containerFiles []string, options types.BuildOptions) (*types.BuildReport, error) {
	return build(ctx, containerFiles, options, "/local/build", prepareLocalRequestBody)
}

// Build performs a container image build on the remote API using the standard build API.
//
// Prepares build contexts and container files by creating tar archives from local directories,
// processes build secrets and authentication, and streams the build to the remote server.
// Supports additional build contexts (URLs, images, local directories) via multipart uploads
// for servers >= v5.6.0, otherwise uses query parameters for compatibility.
//
// Returns a BuildReport containing the final image ID and save format.
func Build(ctx context.Context, containerFiles []string, options types.BuildOptions) (*types.BuildReport, error) {
	return build(ctx, containerFiles, options, "/build", prepareRemoteRequestBody)
}

type prepareRequestBodyFunc func(ctx context.Context, requestParts *RequestParts, buildFilePaths *BuildFilePaths, options types.BuildOptions) (*RequestParts, error)

func build(ctx context.Context, containerFiles []string, options types.BuildOptions, endpoint string, prepareRequestBody prepareRequestBodyFunc) (*types.BuildReport, error) {
	if options.CommonBuildOpts == nil {
		options.CommonBuildOpts = new(define.CommonBuildOptions)
	}

	tempManager := remote_build_helpers.NewTempFileManager()
	defer tempManager.Cleanup()

	params_, err := prepareParams(options)
	if err != nil {
		return nil, err
	}

	var headers http.Header
	var requestBody io.ReadCloser
	requestParts := &RequestParts{
		Params:  params_,
		Headers: headers,
		Body:    requestBody,
	}

	var contextDir string
	if contextDir, err = filepath.EvalSymlinks(options.ContextDirectory); err == nil {
		options.ContextDirectory = contextDir
	}

	requestParts, err = prepareAuthHeaders(options, requestParts)
	if err != nil {
		return nil, err
	}

	contextDirAbs, err := filepath.Abs(options.ContextDirectory)
	if err != nil {
		logrus.Errorf("Cannot find absolute path of %v: %v", options.ContextDirectory, err)
		return nil, err
	}
	stdinDestination := ""
	if endpoint == "/local/build" {
		stdinDestination = contextDirAbs
	}
	buildFilePaths, err := prepareContainerFiles(containerFiles, contextDirAbs, stdinDestination, tempManager)
	if err != nil {
		return nil, err
	}

	if len(buildFilePaths.newContainerFiles) > 0 {
		cFileJSON, err := json.Marshal(buildFilePaths.newContainerFiles)
		if err != nil {
			return nil, err
		}
		requestParts.Params.Set("dockerfile", string(cFileJSON))
	}

	buildFilePaths.excludes = options.Excludes
	if len(buildFilePaths.excludes) == 0 {
		buildFilePaths.excludes, _, err = util.ParseDockerignore(buildFilePaths.newContainerFiles, options.ContextDirectory)
		if err != nil {
			return nil, err
		}
	}

	// build secrets are usually absolute host path or relative to context dir on host
	// in any case move secret to current context and ship the tar.
	secretsForRemote, secretsTarContent, err := prepareSecrets(options.CommonBuildOpts.Secrets, options.ContextDirectory, tempManager)
	if err != nil {
		return nil, err
	}

	if len(secretsForRemote) > 0 {
		c, err := jsoniter.MarshalToString(secretsForRemote)
		if err != nil {
			return nil, err
		}
		requestParts.Params.Add("secrets", c)
		buildFilePaths.tarContent = append(buildFilePaths.tarContent, secretsTarContent...)
	}

	requestParts, err = prepareRequestBody(ctx, requestParts, buildFilePaths, options)
	if err != nil {
		return nil, fmt.Errorf("building tar file: %w", err)
	}
	defer func() {
		if requestParts.Body != nil {
			if err := requestParts.Body.Close(); err != nil {
				logrus.Errorf("failed to close build request body: %v\n", err)
			}
		}
	}()

	response, err := executeBuildRequest(ctx, endpoint, requestParts)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	saveFormat := ldefine.OCIArchive
	if options.OutputFormat == define.Dockerv2ImageManifest {
		saveFormat = ldefine.V2s2Archive
	}

	stdout := io.Writer(os.Stdout)
	if options.Out != nil {
		stdout = options.Out
	}

	return processBuildResponse(response, stdout, saveFormat)
}

func nTar(excludes []string, sources ...string) (io.ReadCloser, error) {
	pm, err := fileutils.NewPatternMatcher(excludes)
	if err != nil {
		return nil, fmt.Errorf("processing excludes list %v: %w", excludes, err)
	}

	if len(sources) == 0 {
		return nil, errors.New("no source(s) provided for build")
	}

	pr, pw := io.Pipe()
	gw := gzip.NewWriter(pw)
	tw := tar.NewWriter(gw)

	var merr *multierror.Error
	go func() {
		defer pw.Close()
		defer gw.Close()
		defer tw.Close()
		seen := make(map[devino]string)
		for i, src := range sources {
			source, err := filepath.Abs(src)
			if err != nil {
				logrus.Errorf("Cannot stat one of source context: %v", err)
				merr = multierror.Append(merr, err)
				return
			}
			err = filepath.WalkDir(source, func(path string, dentry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}

				separator := string(filepath.Separator)
				// check if what we are given is an empty dir, if so then continue w/ it. Else return.
				// if we are given a file or a symlink, we do not want to exclude it.
				if source == path {
					separator = ""
					if dentry.IsDir() {
						var p *os.File
						p, err = os.Open(path)
						if err != nil {
							return err
						}
						defer p.Close()
						_, err = p.Readdir(1)
						if err == nil {
							return nil // non empty root dir, need to return
						}
						if err != io.EOF {
							logrus.Errorf("While reading directory %v: %v", path, err)
						}
					}
				}
				var name string
				if i == 0 {
					name = filepath.ToSlash(strings.TrimPrefix(path, source+separator))
				} else {
					if !dentry.Type().IsRegular() {
						return fmt.Errorf("path %s must be a regular file", path)
					}
					name = filepath.ToSlash(path)
				}
				// If name is absolute path, then it has to be containerfile outside of build context.
				// If not, we should check it for being excluded via pattern matcher.
				if !filepath.IsAbs(name) {
					excluded, err := pm.Matches(name) //nolint:staticcheck
					if err != nil {
						return fmt.Errorf("checking if %q is excluded: %w", name, err)
					}
					if excluded {
						// Note: filepath.SkipDir is not possible to use given .dockerignore semantics.
						// An exception to exclusions may include an excluded directory, therefore we
						// are required to visit all files. :(
						return nil
					}
				}
				switch {
				case dentry.Type().IsRegular(): // add file item
					info, err := dentry.Info()
					if err != nil {
						return err
					}
					di, isHardLink := checkHardLink(info)

					hdr, err := tar.FileInfoHeader(info, "")
					if err != nil {
						return err
					}
					hdr.Uid, hdr.Gid = 0, 0
					orig, ok := seen[di]
					if ok {
						hdr.Typeflag = tar.TypeLink
						hdr.Linkname = orig
						hdr.Size = 0
						hdr.Name = name
						return tw.WriteHeader(hdr)
					}
					f, err := os.Open(path)
					if err != nil {
						return err
					}

					hdr.Name = name
					if err := tw.WriteHeader(hdr); err != nil {
						f.Close()
						return err
					}

					_, err = io.Copy(tw, f)
					f.Close()
					if err == nil && isHardLink {
						seen[di] = name
					}
					return err
				case dentry.IsDir(): // add folders
					info, err := dentry.Info()
					if err != nil {
						return err
					}
					hdr, lerr := tar.FileInfoHeader(info, name)
					if lerr != nil {
						return lerr
					}
					hdr.Name = name
					hdr.Uid, hdr.Gid = 0, 0
					if lerr := tw.WriteHeader(hdr); lerr != nil {
						return lerr
					}
				case dentry.Type()&os.ModeSymlink != 0: // add symlinks as it, not content
					link, err := os.Readlink(path)
					if err != nil {
						return err
					}
					info, err := dentry.Info()
					if err != nil {
						return err
					}
					hdr, lerr := tar.FileInfoHeader(info, link)
					if lerr != nil {
						return lerr
					}
					hdr.Name = name
					hdr.Uid, hdr.Gid = 0, 0
					if lerr := tw.WriteHeader(hdr); lerr != nil {
						return lerr
					}
				} // skip other than file,folder and symlinks
				return nil
			})
			merr = multierror.Append(merr, err)
		}
	}()
	rc := ioutils.NewReadCloserWrapper(pr, func() error {
		if merr != nil {
			merr = multierror.Append(merr, pr.Close())
			return merr.ErrorOrNil()
		}
		return pr.Close()
	})
	return rc, nil
}
