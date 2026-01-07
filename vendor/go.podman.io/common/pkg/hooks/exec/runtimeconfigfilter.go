package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/davecgh/go-spew/spew"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/sirupsen/logrus"
)

var spewConfig = spew.ConfigState{
	Indent:                  " ",
	DisablePointerAddresses: true,
	DisableCapacities:       true,
	SortKeys:                true,
}

type RuntimeConfigFilterOptions struct {
	// The hooks to run
	Hooks []spec.Hook
	// The workdir to change when invoking the hook
	Dir string
	// The container config spec to pass into the hook processes and potentially get modified by them
	Config *spec.Spec
	// Timeout for waiting process killed
	PostKillTimeout time.Duration
}

// RuntimeConfigFilter calls a series of hooks.  But instead of
// passing container state on their standard input,
// RuntimeConfigFilter passes the proposed runtime configuration (and
// reads back a possibly-altered form from their standard output).
//
// Deprecated: Too many arguments, has been refactored and replaced by RuntimeConfigFilterWithOptions instead.
func RuntimeConfigFilter(ctx context.Context, hooks []spec.Hook, config *spec.Spec, postKillTimeout time.Duration) (hookErr, err error) {
	return RuntimeConfigFilterWithOptions(ctx, RuntimeConfigFilterOptions{
		Hooks:           hooks,
		Config:          config,
		PostKillTimeout: postKillTimeout,
	})
}

// RuntimeConfigFilterWithOptions calls a series of hooks.  But instead of
// passing container state on their standard input,
// RuntimeConfigFilterWithOptions passes the proposed runtime configuration (and
// reads back a possibly-altered form from their standard output).
func RuntimeConfigFilterWithOptions(ctx context.Context, options RuntimeConfigFilterOptions) (hookErr, err error) {
	data, err := json.Marshal(options.Config)
	if err != nil {
		return nil, err
	}
	for i, hook := range options.Hooks {
		var stdout bytes.Buffer
		hookErr, err = RunWithOptions(ctx, RunOptions{Hook: &hook, Dir: options.Dir, State: data, Stdout: &stdout, PostKillTimeout: options.PostKillTimeout})
		if err != nil {
			return hookErr, err
		}

		data = stdout.Bytes()
		var newConfig spec.Spec
		err = json.Unmarshal(data, &newConfig)
		if err != nil {
			logrus.Debugf("invalid JSON from config-filter hook %d:\n%s", i, string(data))
			return nil, fmt.Errorf("unmarshal output from config-filter hook %d: %w", i, err)
		}

		if !reflect.DeepEqual(options.Config, &newConfig) {
			oldConfig := spewConfig.Sdump(options.Config)
			newConfig := spewConfig.Sdump(&newConfig)
			diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
				A:        difflib.SplitLines(oldConfig),
				B:        difflib.SplitLines(newConfig),
				FromFile: "Old",
				FromDate: "",
				ToFile:   "New",
				ToDate:   "",
				Context:  1,
			})
			if err == nil {
				logrus.Debugf("precreate hook %d made configuration changes:\n%s", i, diff)
			} else {
				logrus.Warnf("Precreate hook %d made configuration changes, but we could not compute a diff: %v", i, err)
			}
		}

		*options.Config = newConfig
	}

	return nil, nil
}
