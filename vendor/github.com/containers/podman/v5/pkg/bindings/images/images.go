package images

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	handlersTypes "github.com/containers/podman/v5/pkg/api/handlers/types"
	"github.com/containers/podman/v5/pkg/auth"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/reports"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	imageTypes "go.podman.io/image/v5/types"
)

// Exists a lightweight way to determine if an image exists in local storage.  It returns a
// boolean response.
func Exists(ctx context.Context, nameOrID string, _ *ExistsOptions) (bool, error) {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return false, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/images/%s/exists", nil, nil, nameOrID)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	return response.IsSuccess(), nil
}

// List returns a list of images in local storage.  The all boolean and filters parameters are optional
// ways to alter the image query.
func List(ctx context.Context, options *ListOptions) ([]*types.ImageSummary, error) {
	if options == nil {
		options = new(ListOptions)
	}
	var imageSummary []*types.ImageSummary
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/images/json", params, nil)
	if err != nil {
		return imageSummary, err
	}
	defer response.Body.Close()

	return imageSummary, response.Process(&imageSummary)
}

// Get performs an image inspect.  To have the on-disk size of the image calculated, you can
// use the optional size parameter.
func GetImage(ctx context.Context, nameOrID string, options *GetOptions) (*types.ImageInspectReport, error) {
	if options == nil {
		options = new(GetOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	inspectedData := types.ImageInspectReport{}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/images/%s/json", params, nil, nameOrID)
	if err != nil {
		return &inspectedData, err
	}
	defer response.Body.Close()

	return &inspectedData, response.Process(&inspectedData)
}

// Tree retrieves a "tree" based representation of the given image
func Tree(ctx context.Context, nameOrID string, options *TreeOptions) (*types.ImageTreeReport, error) {
	if options == nil {
		options = new(TreeOptions)
	}
	var report types.ImageTreeReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/images/%s/tree", params, nil, nameOrID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}

// History returns the parent layers of an image.
func History(ctx context.Context, nameOrID string, options *HistoryOptions) ([]*handlersTypes.HistoryResponse, error) {
	if options == nil {
		options = new(HistoryOptions)
	}
	_ = options
	var history []*handlersTypes.HistoryResponse
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/images/%s/history", nil, nil, nameOrID)
	if err != nil {
		return history, err
	}
	defer response.Body.Close()

	return history, response.Process(&history)
}

func Load(ctx context.Context, r io.Reader) (*types.ImageLoadReport, error) {
	var report types.ImageLoadReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, r, http.MethodPost, "/images/load", nil, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}

func LoadLocal(ctx context.Context, path string) (*types.ImageLoadReport, error) {
	var report types.ImageLoadReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("path", path)

	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/local/images/load", params, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}

// Export saves images from local storage as a tarball or image archive.  The optional format
// parameter is used to change the format of the output.
func Export(ctx context.Context, nameOrIDs []string, w io.Writer, options *ExportOptions) error {
	if options == nil {
		options = new(ExportOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params, err := options.ToParams()
	if err != nil {
		return err
	}
	for _, ref := range nameOrIDs {
		params.Add("references", ref)
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/images/export", params, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.IsSuccess() || response.IsRedirection() {
		_, err = io.Copy(w, response.Body)
		return err
	}
	return response.Process(nil)
}

// Prune removes unused images from local storage.  The optional filters can be used to further
// define which images should be pruned.
func Prune(ctx context.Context, options *PruneOptions) ([]*reports.PruneReport, error) {
	var (
		deleted []*reports.PruneReport
	)
	if options == nil {
		options = new(PruneOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/images/prune", params, nil)
	if err != nil {
		return deleted, err
	}
	defer response.Body.Close()

	return deleted, response.Process(&deleted)
}

// Tag adds an additional name to locally-stored image. Both the tag and repo parameters are required.
func Tag(ctx context.Context, nameOrID, tag, repo string, options *TagOptions) error {
	if options == nil {
		options = new(TagOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Set("tag", tag)
	params.Set("repo", repo)
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/images/%s/tag", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

// Untag removes a name from locally-stored image. Both the tag and repo parameters are required.
func Untag(ctx context.Context, nameOrID, tag, repo string, options *UntagOptions) error {
	if options == nil {
		options = new(UntagOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Set("tag", tag)
	params.Set("repo", repo)
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/images/%s/untag", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

// Import adds the given image to the local image store.  This can be done by file and the given reader
// or via the url parameter.  Additional metadata can be associated with the image by using the changes and
// message parameters.  The image can also be tagged given a reference. One of url OR r must be provided.
func Import(ctx context.Context, r io.Reader, options *ImportOptions) (*types.ImageImportReport, error) {
	if options == nil {
		options = new(ImportOptions)
	}
	var report types.ImageImportReport
	if r != nil && options.URL != nil {
		return nil, errors.New("url and r parameters cannot be used together")
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, r, http.MethodPost, "/images/import", params, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}

// Search is the binding for libpod's v2 endpoints for Search images.
func Search(ctx context.Context, term string, options *SearchOptions) ([]types.ImageSearchReport, error) {
	if options == nil {
		options = new(SearchOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	params.Set("term", term)

	// SkipTLSVerify is special.  It's not being serialized by ToParams()
	// because we need to flip the boolean.
	if options.SkipTLSVerify != nil {
		params.Set("tlsVerify", strconv.FormatBool(!options.GetSkipTLSVerify()))
	}

	header, err := auth.MakeXRegistryAuthHeader(&imageTypes.SystemContext{AuthFilePath: options.GetAuthfile()}, options.GetUsername(), options.GetPassword())
	if err != nil {
		return nil, err
	}

	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/images/search", params, header)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	results := []types.ImageSearchReport{}
	if err := response.Process(&results); err != nil {
		return nil, err
	}

	return results, nil
}

func Scp(ctx context.Context, source, _ *string, options ScpOptions) (reports.ScpReport, error) {
	rep := reports.ScpReport{}

	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return rep, err
	}
	params, err := options.ToParams()
	if err != nil {
		return rep, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, fmt.Sprintf("/images/scp/%s", *source), params, nil)
	if err != nil {
		return rep, err
	}
	defer response.Body.Close()

	return rep, response.Process(&rep)
}
