package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const dockerHubAPIBaseURL = "https://hub.docker.com"

var dockerHubHTTPClient = &http.Client{Timeout: 15 * time.Second}

// DockerHubTag contains the metadata Docker Hub returns for a repository tag.
type DockerHubTag struct {
	Name          string                `json:"name"`
	FullSize      int64                 `json:"full_size"`
	LastUpdated   string                `json:"last_updated"`
	TagLastPushed string                `json:"tag_last_pushed"`
	Digest        string                `json:"digest"`
	MediaType     string                `json:"media_type"`
	Images        []DockerHubTagVariant `json:"images"`
}

// DockerHubTagVariant describes one platform-specific image behind a tag.
type DockerHubTagVariant struct {
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
	Digest       string `json:"digest"`
	OS           string `json:"os"`
	Size         int64  `json:"size"`
	LastPushed   string `json:"last_pushed"`
}

// SearchImageTags searches Docker Hub tags for an image repository.
func (c *DockerCommand) SearchImageTags(ctx context.Context, imageName, query string) ([]DockerHubTag, error) {
	return searchImageTags(ctx, dockerHubHTTPClient, dockerHubAPIBaseURL, imageName, query)
}

func searchImageTags(ctx context.Context, httpClient *http.Client, baseURL, imageName, query string) ([]DockerHubTag, error) {
	namespace, repository, err := dockerHubRepositoryParts(imageName)
	if err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("build Docker Hub URL: %w", err)
	}
	endpoint.Path = fmt.Sprintf("/v2/namespaces/%s/repositories/%s/tags", url.PathEscape(namespace), url.PathEscape(repository))
	values := endpoint.Query()
	values.Set("page_size", "50")
	if query = strings.TrimSpace(query); query != "" {
		values.Set("name", query)
	}
	endpoint.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build Docker Hub tag request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("search Docker Hub tags: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search Docker Hub tags: %s", response.Status)
	}

	var payload struct {
		Results []DockerHubTag `json:"results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse Docker Hub tags: %w", err)
	}

	return payload.Results, nil
}

func dockerHubRepositoryParts(imageName string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(imageName), "/", 2)
	if len(parts) == 1 && parts[0] != "" {
		return "library", parts[0], nil
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("invalid Docker Hub image name %q", imageName)
}
