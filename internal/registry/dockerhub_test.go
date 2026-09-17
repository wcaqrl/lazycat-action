package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/versioning"
	"github.com/google/go-containerregistry/pkg/name"
	registryserver "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestDockerHubTagMetadataReadsPaginatedUpdateTimes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/namespaces/acme/repositories/widgets/tags" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Query().Get("page") {
		case "1":
			fmt.Fprint(writer, `{"count":2,"results":[{"name":"v1.2.15","last_updated":"2026-07-12T08:30:00Z"}]}`)
		case "2":
			fmt.Fprint(writer, `{"count":2,"results":[{"name":"v1.2.26","last_updated":"2026-06-14T08:30:00Z"}]}`)
		default:
			t.Fatalf("page=%q", request.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	repository, err := name.NewRepository("docker.io/acme/widgets", name.WeakValidation)
	if err != nil {
		t.Fatal(err)
	}
	metadata := dockerHubTagMetadata{client: server.Client(), baseURL: server.URL, pageSize: 1}
	updates, err := metadata.Updates(context.Background(), repository, []string{"v1.2.15", "v1.2.26"}, defaultMaxMatchingTags)
	if err != nil {
		t.Fatal(err)
	}
	if got := updates["v1.2.15"]; !got.Equal(time.Date(2026, 7, 12, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("v1.2.15 updated=%s", got)
	}
	if got := updates["v1.2.26"]; !got.Equal(time.Date(2026, 6, 14, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("v1.2.26 updated=%s", got)
	}
}

func TestDockerHubTagMetadataFailsClosed(t *testing.T) {
	dockerRepository, err := name.NewRepository("docker.io/acme/widgets", name.WeakValidation)
	if err != nil {
		t.Fatal(err)
	}
	otherRepository, err := name.NewRepository("ghcr.io/acme/widgets", name.WeakValidation)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("unsupported registry", func(t *testing.T) {
		_, err := (dockerHubTagMetadata{}).Updates(context.Background(), otherRepository, []string{"v1"}, defaultMaxMatchingTags)
		if err == nil || !strings.Contains(err.Error(), "only for Docker Hub") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := (dockerHubTagMetadata{}).Updates(ctx, dockerRepository, []string{"v1"}, defaultMaxMatchingTags)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	})
	tests := []struct {
		name     string
		status   int
		body     string
		wantText string
	}{
		{name: "HTTP status hides body", status: http.StatusTooManyRequests, body: `{"message":"secret-token"}`, wantText: "HTTP 429"},
		{name: "missing timestamp", status: http.StatusOK, body: `{"count":1,"results":[{"name":"v1"}]}`, wantText: "update time is missing"},
		{name: "missing tag", status: http.StatusOK, body: `{"count":1,"results":[{"name":"other","last_updated":"2026-07-12T08:30:00Z"}]}`, wantText: "missing tag"},
		{name: "too many tags", status: http.StatusOK, body: `{"count":10001,"results":[]}`, wantText: "limit is 10000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				fmt.Fprint(writer, test.body)
			}))
			defer server.Close()
			_, err := (dockerHubTagMetadata{client: server.Client(), baseURL: server.URL}).Updates(context.Background(), dockerRepository, []string{"v1"}, defaultMaxMatchingTags)
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("err=%v", err)
			}
			if strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("response body leaked: %v", err)
			}
		})
	}
	t.Run("bounded response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(writer, strings.Repeat("x", maxDockerHubBodyBytes+1))
		}))
		defer server.Close()
		_, err := (dockerHubTagMetadata{client: server.Client(), baseURL: server.URL}).Updates(context.Background(), dockerRepository, []string{"v1"}, defaultMaxMatchingTags)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestCandidatesInspectsUpdatedTagsUntilPlatformMatches(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := internalTestRepository(t, server.URL, "acme/updated")
	writeInternalTestImage(t, repository.Tag("v1.2.15"), internalTestImageAt(t, "amd64", internalTestDay(12)), server)
	writeInternalTestImage(t, repository.Tag("v1.2.26"), internalTestImageAt(t, "arm64", internalTestDay(14)), server)

	rule := versioning.Rule{
		Channel:      versioning.ChannelStable,
		Sort:         versioning.SortUpdated,
		TagRegex:     regexp.MustCompile(`^v\d+\.\d+\.\d+$`),
		VersionRegex: regexp.MustCompile(`^v(?P<version>\d+\.\d+\.\d+)$`),
	}
	client := New(remote.WithTransport(server.Client().Transport))
	client.tagMetadata = staticTagMetadata{
		"v1.2.15": internalTestDay(12),
		"v1.2.26": internalTestDay(13),
	}
	candidates, err := client.Candidates(context.Background(), repository.Name(), TagFilter{
		Include: rule.TagRegex, UpdatedRule: &rule,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Tag != "v1.2.15" || !candidates[0].Updated.Equal(internalTestDay(12)) {
		t.Fatalf("candidates=%#v", candidates)
	}
}

type staticTagMetadata map[string]time.Time

func (metadata staticTagMetadata) Updates(_ context.Context, _ name.Repository, tags []string, _ int) (map[string]time.Time, error) {
	updates := make(map[string]time.Time, len(tags))
	for _, tag := range tags {
		updates[tag] = metadata[tag]
	}
	return updates, nil
}

func internalTestRepository(t *testing.T, serverURL, path string) name.Repository {
	t.Helper()
	repository, err := name.NewRepository(strings.TrimPrefix(serverURL, "http://")+"/"+path, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func internalTestImageAt(t *testing.T, arch string, created time.Time) v1.Image {
	t.Helper()
	image, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		Architecture: arch,
		OS:           "linux",
		Created:      v1.Time{Time: created},
		Config:       v1.Config{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func internalTestDay(day int) time.Time {
	return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)
}

func writeInternalTestImage(t *testing.T, reference name.Reference, image v1.Image, server *httptest.Server) {
	t.Helper()
	if err := remote.Write(reference, image, remote.WithTransport(server.Client().Transport)); err != nil {
		t.Fatal(err)
	}
}
