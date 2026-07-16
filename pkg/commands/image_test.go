package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
)

func newTestDockerCommand(t *testing.T, handler http.HandlerFunc) *DockerCommand {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithHTTPClient(server.Client()),
		client.WithVersion("1.47"),
	)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	t.Cleanup(func() { _ = dockerClient.Close() })

	return &DockerCommand{Client: dockerClient}
}

func TestSearchImages(t *testing.T) {
	command := newTestDockerCommand(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1.47/images/search", r.URL.Path)
		assert.Equal(t, "postgres", r.URL.Query().Get("term"))
		assert.Equal(t, "50", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"name":"postgres","description":"PostgreSQL","star_count":14000,"is_official":true}]`)
	})

	results, err := command.SearchImages(context.Background(), "postgres")

	if !assert.NoError(t, err) || !assert.Len(t, results, 1) {
		return
	}
	assert.Equal(t, "postgres", results[0].Name)
	assert.True(t, results[0].IsOfficial)
}

func TestPullImage(t *testing.T) {
	command := newTestDockerCommand(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1.47/images/create", r.URL.Path)
		assert.Equal(t, "docker.io/library/postgres", r.URL.Query().Get("fromImage"))
		assert.Equal(t, "16", r.URL.Query().Get("tag"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"Pulling from library/postgres"}`)
		fmt.Fprintln(w, `{"status":"Downloaded newer image"}`)
	})

	assert.NoError(t, command.PullImage("postgres:16"))
}

func TestPullImageReturnsStreamError(t *testing.T) {
	command := newTestDockerCommand(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"errorDetail":{"message":"manifest unknown"},"error":"manifest unknown"}`)
	})

	err := command.PullImage("postgres:missing")

	if !assert.Error(t, err) {
		return
	}
	assert.Equal(t, "manifest unknown", err.Error())
}
