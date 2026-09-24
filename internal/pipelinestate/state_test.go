package pipelinestate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/pipelinestate"
	"github.com/wcaqrl/lazycat-action/internal/source"
)

func TestStateRoundTrip(t *testing.T) {
	filename := filepath.Join(t.TempDir(), ".lazycat-action.lock.yml")
	want := pipelinestate.Lock{
		Source:      source.Candidate{Kind: "git", Ref: "refs/heads/main", Revision: "abc"},
		Fingerprint: "sha256:test", Status: "packaged",
		Application: pipelinestate.Application{PackageID: "cloud.lazycat.example", Version: "1.2.3", LPKSHA256: "deadbeef"},
	}
	if err := pipelinestate.Write(filename, want); err != nil {
		t.Fatal(err)
	}
	got, err := pipelinestate.Read(filename)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != pipelinestate.Version || got.Fingerprint != want.Fingerprint || got.Application.Version != "1.2.3" || got.Source.Revision != "abc" {
		t.Fatalf("got=%#v", got)
	}
}

func TestFingerprintChangesWithRecipeContent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "image"), 0o755); err != nil {
		t.Fatal(err)
	}
	dockerfile := filepath.Join(root, "image", "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Project: config.Project{Root: root},
		Build:   config.Build{Prepare: config.Prepare{Mode: "dockerfile", Context: "image", Dockerfile: "image/Dockerfile", Output: "example/app:{version}"}},
	}
	candidate := source.Candidate{Kind: "oci", Ref: "example/app:1.0.0", Revision: "sha256:source"}
	first, err := pipelinestate.Fingerprint(t.Context(), cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\nLABEL changed=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := pipelinestate.Fingerprint(t.Context(), cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("fingerprint did not change: %s", first)
	}
}

func TestFingerprintChangesWithCoordinatedImageRecipe(t *testing.T) {
	cfg := config.Config{
		Project: config.Project{Root: t.TempDir()},
		Build:   config.Build{Prepare: config.Prepare{Mode: "images"}},
		Images: []config.Image{{
			ID: "server", Target: "service", Service: "immich",
			Source: "ghcr.io/immich-app/immich-server:{tag}", Delivery: config.Delivery{Mode: "lazycat"},
		}},
	}
	candidate := source.Candidate{Kind: "git", Tag: "v3.2.2", Ref: "refs/tags/v3.2.2", Revision: "abc"}
	first, err := pipelinestate.Fingerprint(t.Context(), cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Images[0].Source = "ghcr.io/immich-app/immich-server:v3.2.3"
	second, err := pipelinestate.Fingerprint(t.Context(), cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("fingerprint did not include coordinated image recipe: %s", first)
	}
}
