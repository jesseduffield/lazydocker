package internal

const (
	// BuildahExternalArtifactsDir is the pattern passed to os.MkdirTemp()
	// to generate a temporary directory which will be used to hold
	// external items which are downloaded for a build, typically a tarball
	// being used as an additional build context.
	BuildahExternalArtifactsDir = "buildah-external-artifacts"
	// SourceDateEpochName is both the name of the SOURCE_DATE_EPOCH
	// environment variable and the built-in ARG that carries its value,
	// whether it's read from the environment by our main(), or passed in
	// via CLI or API flags.
	SourceDateEpochName = "SOURCE_DATE_EPOCH"
)

// StageMountDetails holds the Stage/Image mountpoint returned by StageExecutor
// StageExecutor has ability to mount stages/images in current context and
// automatically clean them up.
type StageMountDetails struct {
	DidExecute               bool   // true if this is a freshly-executed stage, or an image, possibly from a non-local cache
	IsStage                  bool   // true if the mountpoint is a stage's rootfs
	IsImage                  bool   // true if the mountpoint is an image's rootfs
	IsAdditionalBuildContext bool   // true if the mountpoint is an additional build context
	MountPoint               string // mountpoint of the stage or image's root directory or path of the additional build context
}
