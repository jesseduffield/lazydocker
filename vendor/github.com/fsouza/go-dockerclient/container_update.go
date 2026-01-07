package docker

import (
	"context"
	"fmt"
	"net/http"
)

// UpdateContainerOptions specify parameters to the UpdateContainer function.
//
// See https://goo.gl/Y6fXUy for more details.
type UpdateContainerOptions struct {
	BlkioWeight        int           `json:"BlkioWeight"`
	CPUShares          int           `json:"CpuShares"`
	CPUPeriod          int           `json:"CpuPeriod"`
	CPURealtimePeriod  int64         `json:"CpuRealtimePeriod"`
	CPURealtimeRuntime int64         `json:"CpuRealtimeRuntime"`
	CPUQuota           int           `json:"CpuQuota"`
	CpusetCpus         string        `json:"CpusetCpus"`
	CpusetMems         string        `json:"CpusetMems"`
	Memory             int           `json:"Memory"`
	MemorySwap         int           `json:"MemorySwap"`
	MemoryReservation  int           `json:"MemoryReservation"`
	KernelMemory       int           `json:"KernelMemory"`
	RestartPolicy      RestartPolicy `json:"RestartPolicy,omitempty"`
	Context            context.Context
}

// UpdateContainer updates the container at ID with the options
//
// See https://goo.gl/Y6fXUy for more details.
func (c *Client) UpdateContainer(id string, opts UpdateContainerOptions) error {
	resp, err := c.do(http.MethodPost, fmt.Sprintf("/containers/%s/update", id), doOptions{
		data:      opts,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
