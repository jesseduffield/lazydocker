package entities

import (
	"context"
	"io"

	"github.com/containers/podman/v5/pkg/domain/entities/reports"
	"go.podman.io/common/libimage/define"
	"go.podman.io/common/pkg/config"
)

type ImageEngine interface { //nolint:interfacebloat
	ArtifactAdd(ctx context.Context, name string, artifactBlobs []ArtifactBlob, opts ArtifactAddOptions) (*ArtifactAddReport, error)
	ArtifactExtract(ctx context.Context, name string, target string, opts ArtifactExtractOptions) error
	ArtifactExtractTarStream(ctx context.Context, w io.Writer, name string, opts ArtifactExtractOptions) error
	ArtifactInspect(ctx context.Context, name string, opts ArtifactInspectOptions) (*ArtifactInspectReport, error)
	ArtifactList(ctx context.Context, opts ArtifactListOptions) ([]*ArtifactListReport, error)
	ArtifactPull(ctx context.Context, name string, opts ArtifactPullOptions) (*ArtifactPullReport, error)
	ArtifactPush(ctx context.Context, name string, opts ArtifactPushOptions) (*ArtifactPushReport, error)
	ArtifactRm(ctx context.Context, opts ArtifactRemoveOptions) (*ArtifactRemoveReport, error)
	Build(ctx context.Context, containerFiles []string, opts BuildOptions) (*BuildReport, error)
	Config(ctx context.Context) (*config.Config, error)
	Exists(ctx context.Context, nameOrID string) (*BoolReport, error)
	History(ctx context.Context, nameOrID string, opts ImageHistoryOptions) (*ImageHistoryReport, error)
	Import(ctx context.Context, opts ImageImportOptions) (*ImageImportReport, error)
	Inspect(ctx context.Context, namesOrIDs []string, opts InspectOptions) ([]*ImageInspectReport, []error, error)
	List(ctx context.Context, opts ImageListOptions) ([]*ImageSummary, error)
	Load(ctx context.Context, opts ImageLoadOptions) (*ImageLoadReport, error)
	Mount(ctx context.Context, images []string, options ImageMountOptions) ([]*ImageMountReport, error)
	Prune(ctx context.Context, opts ImagePruneOptions) ([]*reports.PruneReport, error)
	Pull(ctx context.Context, rawImage string, opts ImagePullOptions) (*ImagePullReport, error)
	Push(ctx context.Context, source string, destination string, opts ImagePushOptions) (*ImagePushReport, error)
	Remove(ctx context.Context, images []string, opts ImageRemoveOptions) (*ImageRemoveReport, []error)
	Save(ctx context.Context, nameOrID string, tags []string, options ImageSaveOptions) error
	Scp(ctx context.Context, src, dst string, opts ImageScpOptions) (*ImageScpReport, error)
	Search(ctx context.Context, term string, opts ImageSearchOptions) ([]ImageSearchReport, error)
	SetTrust(ctx context.Context, args []string, options SetTrustOptions) error
	ShowTrust(ctx context.Context, args []string, options ShowTrustOptions) (*ShowTrustReport, error)
	Shutdown(ctx context.Context)
	Tag(ctx context.Context, nameOrID string, tags []string, options ImageTagOptions) error
	Tree(ctx context.Context, nameOrID string, options ImageTreeOptions) (*ImageTreeReport, error)
	Unmount(ctx context.Context, images []string, options ImageUnmountOptions) ([]*ImageUnmountReport, error)
	Untag(ctx context.Context, nameOrID string, tags []string, options ImageUntagOptions) error
	ManifestCreate(ctx context.Context, name string, images []string, opts ManifestCreateOptions) (string, error)
	ManifestExists(ctx context.Context, name string) (*BoolReport, error)
	ManifestInspect(ctx context.Context, name string, opts ManifestInspectOptions) (*define.ManifestListData, error)
	ManifestAdd(ctx context.Context, listName string, imageNames []string, opts ManifestAddOptions) (string, error)
	ManifestAddArtifact(ctx context.Context, name string, files []string, opts ManifestAddArtifactOptions) (string, error)
	ManifestAnnotate(ctx context.Context, names, image string, opts ManifestAnnotateOptions) (string, error)
	ManifestRemoveDigest(ctx context.Context, names, image string) (string, error)
	ManifestRm(ctx context.Context, names []string, imageRmOpts ImageRemoveOptions) (*ImageRemoveReport, []error)
	ManifestPush(ctx context.Context, name, destination string, imagePushOpts ImagePushOptions) (string, error)
	ManifestListClear(ctx context.Context, name string) (string, error)
	Sign(ctx context.Context, names []string, options SignOptions) (*SignReport, error)
	FarmNodeName(ctx context.Context) string
	FarmNodeDriver(ctx context.Context) string
	FarmNodeInspect(ctx context.Context) (*FarmInspectReport, error)
}
