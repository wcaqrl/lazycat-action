package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
)

const (
	dockerHubBaseURL        = "https://hub.docker.com"
	dockerHubPageSize       = 100
	dockerHubRequestTimeout = 30 * time.Second
	maxDockerHubBodyBytes   = 4 << 20
)

type tagMetadata interface {
	Updates(context.Context, name.Repository, []string, int) (map[string]time.Time, error)
}

type dockerHubTagMetadata struct {
	client   *http.Client
	baseURL  string
	pageSize int
}

type dockerHubTagsPage struct {
	Count   int                  `json:"count"`
	Results []dockerHubTagResult `json:"results"`
}

type dockerHubTagResult struct {
	Name        string    `json:"name"`
	LastUpdated time.Time `json:"last_updated"`
}

func (metadata dockerHubTagMetadata) Updates(ctx context.Context, repository name.Repository, tags []string, maxTags int) (map[string]time.Time, error) {
	if ctx == nil {
		return nil, errors.New("docker hub metadata context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("docker hub metadata request cancelled: %w", err)
	}
	if !isDockerHubRegistry(repository.RegistryStr()) {
		return nil, fmt.Errorf("updated sorting is supported only for Docker Hub repositories, got %q", repository.RegistryStr())
	}
	desired := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			desired[tag] = struct{}{}
		}
	}
	if len(desired) > maxTags {
		return nil, fmt.Errorf("docker hub metadata requested %d tags; limit is %d", len(desired), maxTags)
	}
	if len(desired) == 0 {
		return map[string]time.Time{}, nil
	}

	parts := strings.SplitN(repository.RepositoryStr(), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("parse Docker Hub repository path %q", repository.RepositoryStr())
	}
	pageSize := metadata.pageSize
	if pageSize <= 0 || pageSize > dockerHubPageSize {
		pageSize = dockerHubPageSize
	}
	baseURL := strings.TrimRight(metadata.baseURL, "/")
	if baseURL == "" {
		baseURL = dockerHubBaseURL
	}
	client := metadata.client
	if client == nil {
		client = &http.Client{Timeout: dockerHubRequestTimeout}
	}

	updates := make(map[string]time.Time, len(desired))
	for page := 1; page <= (maxTags+pageSize-1)/pageSize; page++ {
		endpoint := fmt.Sprintf(
			"%s/v2/namespaces/%s/repositories/%s/tags?page=%s&page_size=%s",
			baseURL,
			url.PathEscape(parts[0]),
			url.PathEscape(parts[1]),
			strconv.Itoa(page),
			strconv.Itoa(pageSize),
		)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("create Docker Hub tag metadata request: %w", err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "lazycat-github-action")
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("request Docker Hub tag metadata for %q: %w", repository.Name(), err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxDockerHubBodyBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("read Docker Hub tag metadata for %q: %w", repository.Name(), errors.Join(readErr, closeErr))
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("docker hub tag metadata for %q returned HTTP %d", repository.Name(), response.StatusCode)
		}
		if len(body) > maxDockerHubBodyBytes {
			return nil, fmt.Errorf("docker hub tag metadata for %q exceeds %d bytes", repository.Name(), maxDockerHubBodyBytes)
		}
		var payload dockerHubTagsPage
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode Docker Hub tag metadata for %q: %w", repository.Name(), err)
		}
		if payload.Count > maxTags {
			return nil, fmt.Errorf("docker hub repository %q returned %d tags; limit is %d", repository.Name(), payload.Count, maxTags)
		}
		for _, result := range payload.Results {
			if _, wanted := desired[result.Name]; !wanted {
				continue
			}
			if result.LastUpdated.IsZero() {
				return nil, fmt.Errorf("docker hub tag %q update time is missing", result.Name)
			}
			updates[result.Name] = result.LastUpdated.UTC()
		}
		if len(updates) == len(desired) {
			return updates, nil
		}
		if len(payload.Results) == 0 || len(payload.Results) < pageSize || (payload.Count > 0 && page*pageSize >= payload.Count) {
			break
		}
	}
	for tag := range desired {
		if _, found := updates[tag]; !found {
			return nil, fmt.Errorf("docker hub tag metadata for %q is missing tag %q", repository.Name(), tag)
		}
	}
	return updates, nil
}

func isDockerHubRegistry(registry string) bool {
	switch strings.ToLower(strings.TrimSpace(registry)) {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return true
	default:
		return false
	}
}
