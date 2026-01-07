package containers

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

// Checkpoint checkpoints the given container (identified by nameOrID).  All additional
// options are options and allow for more fine grained control of the checkpoint process.
func Checkpoint(ctx context.Context, nameOrID string, options *CheckpointOptions) (*types.CheckpointReport, error) {
	var report types.CheckpointReport
	if options == nil {
		options = new(CheckpointOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}

	// "export" is a bool for the server so override it in the parameters
	// if set.
	export := false
	if options.Export != nil && *options.Export != "" {
		export = true
		params.Set("export", "true")
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/checkpoint", params, nil, nameOrID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK || !export {
		return &report, response.Process(&report)
	}

	f, err := os.OpenFile(*options.Export, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := io.Copy(f, response.Body); err != nil {
		return nil, err
	}

	return &types.CheckpointReport{}, nil
}

// Restore restores a checkpointed container to running. The container is identified by the nameOrID option. All
// additional options are optional and allow finer control of the restore process.
func Restore(ctx context.Context, nameOrID string, options *RestoreOptions) (*types.RestoreReport, error) {
	var report types.RestoreReport
	if options == nil {
		options = new(RestoreOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}

	for _, p := range options.PublishPorts {
		params.Add("publishPorts", p)
	}

	params.Del("ImportArchive") // The import key is a reserved golang term

	// Open the to-be-imported archive if needed.
	var r io.Reader
	i := options.GetImportArchive()
	if i == "" {
		// backwards compat, ImportAchive is a typo but we still have to
		// support this to avoid breaking users
		// TODO: remove ImportAchive with 5.0
		i = options.GetImportAchive()
	}
	if i != "" {
		params.Set("import", "true")
		r, err = os.Open(i)
		if err != nil {
			return nil, err
		}
		// Hard-code the name since it will be ignored in any case.
		nameOrID = "import"
	}

	response, err := conn.DoRequest(ctx, r, http.MethodPost, "/containers/%s/restore", params, nil, nameOrID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}
