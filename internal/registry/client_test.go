package registry_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/platform"
	internalregistry "github.com/wcaqrl/lazycat-action/internal/registry"
	"github.com/wcaqrl/lazycat-action/internal/versioning"
	"github.com/google/go-containerregistry/pkg/name"
	registryserver "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestClientSelectsLinuxAMD64FromMultiPlatformIndex(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := testRepository(t, server.URL, "acme/app")

	amdCreated := time.Date(2026, 7, 10, 15, 30, 20, 0, time.UTC)
	armCreated := time.Date(2026, 7, 11, 15, 30, 20, 0, time.UTC)
	amdImage := imageAt(t, "amd64", amdCreated)
	armImage := imageAt(t, "arm64", armCreated)
	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: amdImage, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: armImage, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	writeIndex(t, repository.Tag("v1.2.3"), index, server)

	client := internalregistry.New(remote.WithTransport(server.Client().Transport))
	result, err := client.Inspect(context.Background(), repository.Tag("v1.2.3").Name())
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := amdImage.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != wantDigest.String() || !result.Created.Equal(amdCreated) || result.Platform != "linux/amd64" {
		t.Fatalf("result=%#v want digest=%s created=%s", result, wantDigest, amdCreated)
	}
}

func TestClientSelectsConfiguredARM64Target(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := testRepository(t, server.URL, "acme/arm-target")

	amdImage := imageAt(t, "amd64", atDay(1))
	armImage := imageAt(t, "arm64", atDay(2))
	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: amdImage, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: armImage, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	writeIndex(t, repository.Tag("v1.2.3"), index, server)

	client := internalregistry.New(remote.WithTransport(server.Client().Transport))
	target := platform.Target{OS: "linux", Arch: "arm64"}
	result, err := client.InspectTarget(context.Background(), repository.Tag("v1.2.3").Name(), target)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := armImage.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != wantDigest.String() || result.Platform != "linux/arm64" || !result.Created.Equal(atDay(2)) {
		t.Fatalf("result=%#v", result)
	}
}

func TestClientListsCandidatesWithPlatformSpecificMetadata(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := testRepository(t, server.URL, "acme/list")
	for _, item := range []struct {
		tag     string
		created time.Time
	}{
		{tag: "v1.0.0", created: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{tag: "v2.0.0-beta.1", created: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
		{tag: "nightly", created: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)},
	} {
		writeImage(t, repository.Tag(item.tag), imageAt(t, "amd64", item.created), server)
	}

	client := internalregistry.New(remote.WithTransport(server.Client().Transport))
	candidates, err := client.Candidates(context.Background(), repository.Name())
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Tag < candidates[j].Tag })
	if len(candidates) != 3 || candidates[0].Tag != "nightly" || candidates[1].Tag != "v1.0.0" || candidates[2].Tag != "v2.0.0-beta.1" {
		t.Fatalf("candidates=%#v", candidates)
	}
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Digest, "sha256:") || candidate.Created.IsZero() {
			t.Fatalf("candidate=%#v", candidate)
		}
	}
}

func TestClientRejectsIndexWithoutLinuxAMD64(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := testRepository(t, server.URL, "acme/arm-only")
	armImage := imageAt(t, "arm64", time.Now().UTC())
	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: armImage, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	writeIndex(t, repository.Tag("latest"), index, server)
	client := internalregistry.New(remote.WithTransport(server.Client().Transport))
	if _, err := client.Inspect(context.Background(), repository.Tag("latest").Name()); err == nil || !errors.Is(err, internalregistry.ErrPlatformNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestCandidatesSkipsArmOnlyTagsWhenAMD64CandidatesExist(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := testRepository(t, server.URL, "acme/platforms")
	writeImage(t, repository.Tag("v1.2.3"), imageAt(t, "amd64", atDay(1)), server)
	writeImage(t, repository.Tag("v2.0.0-arm64"), imageAt(t, "arm64", atDay(2)), server)

	client := internalregistry.New(remote.WithTransport(server.Client().Transport))
	candidates, err := client.Candidates(context.Background(), repository.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Tag != "v1.2.3" {
		t.Fatalf("candidates=%#v", candidates)
	}
}

func TestClientHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := internalregistry.New()
	if _, err := client.Inspect(ctx, "registry.example.invalid/acme/app:latest"); err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestCandidatesFiltersTagsBeforePlatformInspection(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := testRepository(t, server.URL, "acme/mixed")
	writeImage(t, repository.Tag("v1.2.3"), imageAt(t, "amd64", atDay(1)), server)
	writeImage(t, repository.Tag("windows-latest"), imageAtOS(t, "windows", "amd64", atDay(2)), server)

	client := internalregistry.New(remote.WithTransport(server.Client().Transport))
	candidates, err := client.Candidates(context.Background(), repository.Name(), internalregistry.TagFilter{Exclude: regexp.MustCompile(`windows`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Tag != "v1.2.3" {
		t.Fatalf("candidates=%#v", candidates)
	}
}

func TestCandidatesInspectsSemVerTagsInRankOrderUntilPlatformMatches(t *testing.T) {
	server := httptest.NewServer(registryserver.New())
	defer server.Close()
	repository := testRepository(t, server.URL, "acme/ranked")
	writeImage(t, repository.Tag("v1.0.0"), imageAt(t, "amd64", atDay(1)), server)
	writeImage(t, repository.Tag("v2.0.0"), imageAt(t, "amd64", atDay(2)), server)
	writeImage(t, repository.Tag("v3.0.0"), imageAt(t, "arm64", atDay(3)), server)

	rule := versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortSemVer, VersionTemplate: "{version}"}
	client := internalregistry.New(remote.WithTransport(server.Client().Transport))
	candidates, err := client.Candidates(context.Background(), repository.Name(), internalregistry.TagFilter{
		Include: regexp.MustCompile(`^v\d+\.\d+\.\d+$`), SemVerRule: &rule,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Tag != "v2.0.0" {
		t.Fatalf("candidates=%#v", candidates)
	}
}

func testRepository(t *testing.T, serverURL, path string) name.Repository {
	t.Helper()
	repository, err := name.NewRepository(strings.TrimPrefix(serverURL, "http://")+"/"+path, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func imageAt(t *testing.T, arch string, created time.Time) v1.Image {
	return imageAtOS(t, "linux", arch, created)
}

func imageAtOS(t *testing.T, osName, arch string, created time.Time) v1.Image {
	t.Helper()
	image, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		Architecture: arch,
		OS:           osName,
		Created:      v1.Time{Time: created},
		Config:       v1.Config{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func atDay(day int) time.Time {
	return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)
}

func writeImage(t *testing.T, reference name.Reference, image v1.Image, server *httptest.Server) {
	t.Helper()
	if err := remote.Write(reference, image, remote.WithTransport(server.Client().Transport)); err != nil {
		t.Fatal(err)
	}
}

func writeIndex(t *testing.T, reference name.Reference, index v1.ImageIndex, server *httptest.Server) {
	t.Helper()
	if err := remote.WriteIndex(reference, index, remote.WithTransport(server.Client().Transport)); err != nil {
		t.Fatal(err)
	}
}
