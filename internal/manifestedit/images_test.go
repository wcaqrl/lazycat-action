package manifestedit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
)

func TestReadAndApplyUpdatesOnlyExplicitService(t *testing.T) {
	filename := writeManifest(t, `application:
  subdomain: app
services:
  db:
    image: postgres:17
  web:
    # retain this comment
    # upstream: ghcr.io/acme/web:v1.0.0
    image: registry.lazycat.cloud/acme/web:v1.0.0 # runtime
`)
	target := manifestedit.Target{ID: "web", Kind: manifestedit.TargetService, Service: "web"}
	current, err := manifestedit.Read(filename, []manifestedit.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || current[0].RuntimeRef != "registry.lazycat.cloud/acme/web:v1.0.0" || current[0].UpstreamRef != "ghcr.io/acme/web:v1.0.0" {
		t.Fatalf("current=%#v", current)
	}

	changes, err := manifestedit.Apply(filename, []manifestedit.Update{{
		Target: target, SourceRef: "ghcr.io/acme/web:v1.2.3", RuntimeRef: "registry.lazycat.cloud/acme/web:v1.2.3",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Changed || changes[0].OldUpstreamRef != "ghcr.io/acme/web:v1.0.0" {
		t.Fatalf("changes=%#v", changes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, expected := range []string{
		"image: postgres:17",
		"# retain this comment",
		"# upstream: ghcr.io/acme/web:v1.2.3",
		"image: registry.lazycat.cloud/acme/web:v1.2.3 # runtime",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output missing %q:\n%s", expected, got)
		}
	}
}

func TestApplyApplicationImageAndInsertMissingServiceImage(t *testing.T) {
	filename := writeManifest(t, `application:
  subdomain: app
services:
  worker:
    command: run
`)
	changes, err := manifestedit.Apply(filename, []manifestedit.Update{
		{Target: manifestedit.Target{ID: "runtime", Kind: manifestedit.TargetApplication}, SourceRef: "ghcr.io/acme/runtime:v1", RuntimeRef: "mirror/acme/runtime:v1"},
		{Target: manifestedit.Target{ID: "worker", Kind: manifestedit.TargetService, Service: "worker"}, SourceRef: "ghcr.io/acme/worker:v1", RuntimeRef: "ghcr.io/acme/worker:v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || !changes[0].Changed || !changes[1].Changed {
		t.Fatalf("changes=%#v", changes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, expected := range []string{"upstream: ghcr.io/acme/runtime:v1", "image: mirror/acme/runtime:v1", "upstream: ghcr.io/acme/worker:v1", "image: ghcr.io/acme/worker:v1"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output missing %q:\n%s", expected, got)
		}
	}
}

func TestReadAndApplySupportsTemplateControls(t *testing.T) {
	filename := writeManifest(t, `application:
  subdomain: nowledge-mem
services:
  mem:
    # nowledgelabs/mem:0.10.22-vulkan
    image: registry.lazycat.cloud/czyt/nowledgelabs/mem:2dae0c898a81ea1e
  nowledge-mem-snap:
    # czyt/nowledge-mem-snap:v0.2.5
    image: registry.lazycat.cloud/czyt/czyt/nowledge-mem-snap:588a01b8b8a76699
    environment:
      - NMEM_SNAP_OIDC_REDIRECT_URL=https://${LAZYCAT_APP_DOMAIN}/snap/auth/oidc/callback
{{- if .U.snap_oidc_allowed_domains }}
      - NMEM_SNAP_OIDC_ALLOWED_DOMAINS={{ .U.snap_oidc_allowed_domains }}
{{- end }}
`)
	mem := manifestedit.Target{ID: "mem", Kind: manifestedit.TargetService, Service: "mem"}
	snap := manifestedit.Target{ID: "nowledge-mem-snap", Kind: manifestedit.TargetService, Service: "nowledge-mem-snap"}

	current, err := manifestedit.Read(filename, []manifestedit.Target{mem, snap})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 2 || current[0].RuntimeRef != "registry.lazycat.cloud/czyt/nowledgelabs/mem:2dae0c898a81ea1e" || current[1].RuntimeRef != "registry.lazycat.cloud/czyt/czyt/nowledge-mem-snap:588a01b8b8a76699" {
		t.Fatalf("current=%#v", current)
	}

	_, err = manifestedit.Apply(filename, []manifestedit.Update{{
		Target:     mem,
		SourceRef:  "docker.io/nowledgelabs/mem:0.10.23-vulkan",
		RuntimeRef: "registry.lazycat.cloud/czyt/nowledgelabs/mem:0.10.23-vulkan",
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, expected := range []string{
		"# upstream: docker.io/nowledgelabs/mem:0.10.23-vulkan",
		"image: registry.lazycat.cloud/czyt/nowledgelabs/mem:0.10.23-vulkan",
		"image: registry.lazycat.cloud/czyt/czyt/nowledge-mem-snap:588a01b8b8a76699",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output missing %q:\n%s", expected, got)
		}
	}
	for _, control := range []string{
		"{{- if .U.snap_oidc_allowed_domains }}",
		"{{- end }}",
	} {
		if !hasExactLine(got, control) {
			t.Fatalf("output missing exact template control %q:\n%s", control, got)
		}
	}
}

func TestApplyValidatesAllTargetsBeforeWriting(t *testing.T) {
	original := `application:
  subdomain: app
services:
  web:
    image: old
`
	filename := writeManifest(t, original)
	_, err := manifestedit.Apply(filename, []manifestedit.Update{
		{Target: manifestedit.Target{ID: "web", Kind: manifestedit.TargetService, Service: "web"}, SourceRef: "source:new", RuntimeRef: "runtime:new"},
		{Target: manifestedit.Target{ID: "missing", Kind: manifestedit.TargetService, Service: "missing"}, SourceRef: "source:new", RuntimeRef: "runtime:new"},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err=%v", err)
	}
	data, readErr := os.ReadFile(filename)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("manifest changed after validation failure:\n%s", data)
	}
}

func TestReadRejectsDuplicateTargetsAndSymlink(t *testing.T) {
	filename := writeManifest(t, "application:\n  subdomain: app\n")
	target := manifestedit.Target{ID: "runtime", Kind: manifestedit.TargetApplication}
	if _, err := manifestedit.Read(filename, []manifestedit.Target{target, target}); err == nil {
		t.Fatal("expected duplicate target to fail")
	}
	symlink := filepath.Join(t.TempDir(), "manifest.yml")
	if err := os.Symlink(filename, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := manifestedit.Read(symlink, []manifestedit.Target{target}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	filename := writeManifest(t, `application:
  # upstream: ghcr.io/acme/runtime:v1
  image: mirror/acme/runtime:v1
  subdomain: app
`)
	changes, err := manifestedit.Apply(filename, []manifestedit.Update{{
		Target:    manifestedit.Target{ID: "runtime", Kind: manifestedit.TargetApplication},
		SourceRef: "ghcr.io/acme/runtime:v1", RuntimeRef: "mirror/acme/runtime:v1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Changed {
		t.Fatalf("changes=%#v", changes)
	}
}

func writeManifest(t *testing.T, contents string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "lzc-manifest.yml")
	if err := os.WriteFile(filename, []byte(contents), 0o640); err != nil {
		t.Fatal(err)
	}
	return filename
}

func hasExactLine(contents, expected string) bool {
	for _, line := range strings.Split(contents, "\n") {
		if line == expected {
			return true
		}
	}
	return false
}
