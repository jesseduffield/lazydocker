package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSearchImageTagsUsesOfficialNamespaceAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/namespaces/library/repositories/nginx/tags", r.URL.Path)
		assert.Equal(t, "alpine", r.URL.Query().Get("name"))
		assert.Equal(t, "50", r.URL.Query().Get("page_size"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"name":"alpine","full_size":75270706,"digest":"sha256:abc","images":[{"architecture":"amd64","os":"linux","size":75270706}]}]}`)
	}))
	defer server.Close()

	tags, err := searchImageTags(context.Background(), server.Client(), server.URL, "nginx", " alpine ")

	assert.NoError(t, err)
	if assert.Len(t, tags, 1) {
		assert.Equal(t, "alpine", tags[0].Name)
		assert.EqualValues(t, 75270706, tags[0].FullSize)
		assert.Equal(t, "amd64", tags[0].Images[0].Architecture)
	}
}

func TestSearchImageTagsUsesRepositoryNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/namespaces/bitnami/repositories/nginx/tags", r.URL.Path)
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer server.Close()

	_, err := searchImageTags(context.Background(), server.Client(), server.URL, "bitnami/nginx", "")

	assert.NoError(t, err)
}
