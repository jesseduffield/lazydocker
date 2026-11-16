package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type DockerHubResult struct {
	RepoName         string `json:"repo_name"`
	ShortDescription string `json:"short_description"`
	StarCount        int    `json:"star_count"`
	PullCount        int    `json:"pull_count"`
	IsOfficial       bool   `json:"is_official"`
	IsAutomated      bool   `json:"is_automated"`
}

type dockerHubSearchResponse struct {
	Count   int               `json:"count"`
	Results []DockerHubResult `json:"results"`
}

func SearchDockerHub(query string, pageSize int) ([]DockerHubResult, error) {
	if pageSize <= 0 {
		pageSize = 25
	}

	apiURL := fmt.Sprintf(
		"https://hub.docker.com/v2/search/repositories/?query=%s&page_size=%d",
		url.QueryEscape(query),
		pageSize,
	)

	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build Docker Hub request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "lazydocker")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Docker Hub search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Docker Hub returned non-OK status: %s", resp.Status)
	}

	var searchResp dockerHubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse Docker Hub response: %w", err)
	}

	return searchResp.Results, nil
}
