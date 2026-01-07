package docker

import (
	"context"
	"fmt"
	"net/http"
)

// RenameContainerOptions specify parameters to the RenameContainer function.
//
// See https://goo.gl/46inai for more details.
type RenameContainerOptions struct {
	// ID of container to rename
	ID string `qs:"-"`

	// New name
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	Context context.Context
}

// RenameContainer updates and existing containers name
//
// See https://goo.gl/46inai for more details.
func (c *Client) RenameContainer(opts RenameContainerOptions) error {
	resp, err := c.do(http.MethodPost, fmt.Sprintf("/containers/"+opts.ID+"/rename?%s", queryString(opts)), doOptions{
		context: opts.Context,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
