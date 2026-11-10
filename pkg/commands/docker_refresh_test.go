package commands

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// newTestDockerClient builds a docker client talking to the given test HTTP
// server instead of a real daemon, with a pinned API version so no version
// negotiation ping is needed.
func newTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	cli, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.44"),
	)
	if err != nil {
		t.Fatalf("failed to create test docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

// TestSetContainerDetailsBoundsConcurrentInspects is a regression test for
// https://github.com/jesseduffield/lazydocker/issues/849: SetContainerDetails
// used to spawn one goroutine per container doing a ContainerInspect with no
// bound on how many inspects were in flight at once, so a refresh on a host
// with many containers fanned out into an unbounded pile of requests
// against dockerd. The number of concurrent inspects must stay bounded even
// when there are many more containers than the bound.
func TestSetContainerDetailsBoundsConcurrentInspects(t *testing.T) {
	const containerCount = 20

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		for {
			max := maxInFlight.Load()
			if current <= max || maxInFlight.CompareAndSwap(max, current) {
				break
			}
		}
		defer inFlight.Add(-1)

		// Simulate a slow daemon so the inspects overlap.
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"Id": %q}`, r.URL.Path)
	}))
	defer server.Close()

	c := &DockerCommand{
		Client: newTestDockerClient(t, server),
		Log:    logrus.NewEntry(logrus.New()),
		Config: &config.AppConfig{},
	}

	containers := make([]*Container, containerCount)
	for i := range containers {
		containers[i] = &Container{ID: fmt.Sprintf("%064x", i+1)}
	}

	c.SetContainerDetails(containers)

	assert.LessOrEqual(t, maxInFlight.Load(), int32(maxConcurrentContainerInspects),
		"concurrent ContainerInspect calls must stay bounded")
	assert.Greater(t, maxInFlight.Load(), int32(1),
		"inspects should still run concurrently up to the bound")
	for _, ctr := range containers {
		assert.NotEmpty(t, ctr.Details.ID, "each container should have its inspect details attached")
	}
}

// TestRefreshImagesTimesOut is a regression test for
// https://github.com/jesseduffield/lazydocker/issues/849: RefreshImages used
// to call ImageList with context.Background() and no timeout, so when the
// daemon could not keep up the calls piled up without bound. A hung image
// list call must fail fast instead of blocking forever.
func TestRefreshImagesTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a daemon that never answers the image list.
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	c := &DockerCommand{
		Client: newTestDockerClient(t, server),
		Log:    logrus.NewEntry(logrus.New()),
		Config: &config.AppConfig{},
	}

	oldTimeout := dockerRequestTimeout
	dockerRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { dockerRequestTimeout = oldTimeout })

	start := time.Now()
	_, err := c.RefreshImages()
	elapsed := time.Since(start)

	assert.Error(t, err, "a hung image list call must return an error")
	assert.Less(t, elapsed, 1*time.Second,
		"RefreshImages must time out instead of waiting on the daemon; took %v", elapsed)
}
