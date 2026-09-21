package prepare_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/prepare"
	"github.com/wcaqrl/lazycat-action/internal/source"
)

func TestPassthroughPinsOCIDigest(t *testing.T) {
	result, err := (prepare.Runner{}).Prepare(t.Context(), prepare.Request{
		ProjectRoot: t.TempDir(),
		Config:      config.Config{Source: config.Source{Kind: config.SourceKindOCI}, Build: config.Build{Prepare: config.Prepare{Mode: "passthrough"}}},
		Candidate:   source.Candidate{Kind: "oci", Ref: "docker.io/acme/app:1.2.3", Revision: "sha256:abc"},
		Target:      platform.Target{OS: "linux", Arch: "amd64"},
	})
	if err != nil || result.Image != "docker.io/acme/app:1.2.3@sha256:abc" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestCommandReceivesSourceAndApplicationVersions(t *testing.T) {
	root := t.TempDir()
	called := false
	runner := prepare.Runner{Run: func(_ context.Context, directory string, command, environment []string, _, _ io.Writer) error {
		called = true
		if directory != root || !strings.Contains(strings.Join(command, " "), "./scripts/build.sh") {
			t.Fatalf("directory=%q command=%v", directory, command)
		}
		joined := strings.Join(environment, "\n")
		for _, expected := range []string{
			"LAZYCAT_SOURCE_VERSION=2.0.0", "LAZYCAT_APPLICATION_VERSION=3.1.5",
			"LAZYCAT_OUTPUT_IMAGE=ghcr.io/acme/app:3.1.5-abcdef012345",
		} {
			if !strings.Contains(joined, expected) {
				t.Fatalf("environment missing %q: %s", expected, joined)
			}
		}
		return nil
	}}
	result, err := runner.Prepare(t.Context(), prepare.Request{
		ProjectRoot: root,
		Config: config.Config{
			Source: config.Source{Kind: config.SourceKindOCI},
			Build:  config.Build{Prepare: config.Prepare{Mode: "command", Command: "./scripts/build.sh", Output: "ghcr.io/acme/app:{version}-{fingerprint}"}},
		},
		Candidate:          source.Candidate{Kind: "oci", Version: "2.0.0", Ref: "docker.io/acme/app:2.0.0", Revision: "sha256:abc"},
		ApplicationVersion: "3.1.5", Fingerprint: "sha256:abcdef0123456789",
		Target: platform.Target{OS: "linux", Arch: "amd64"},
	})
	if err != nil || !called || result.Image != "ghcr.io/acme/app:3.1.5-abcdef012345" || filepath.Clean(result.SourceDir) != "." {
		t.Fatalf("called=%t result=%#v err=%v", called, result, err)
	}
}
