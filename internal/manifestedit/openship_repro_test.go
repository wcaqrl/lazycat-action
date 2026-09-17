package manifestedit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
)

func TestOpenShipInlineConditionalEnvironmentSurvivesImageUpdateAndBuild(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"lzc-build.yml": "manifest: ./lzc-manifest.yml\n",
		"package.yml": `package: community.lazycat.app.openshipfixture
version: 0.3.0
name: Openship Fixture
description: Openship inline conditional fixture
`,
		"lzc-manifest.yml": `application:
  subdomain: openship-fixture
services:
  api:
    # upstream: ghcr.io/dockers-x/openship-api:0.4.4
    image: registry.lazycat.cloud/czyt/dockers-x/openship-api:old
    environment:
      - NODE_ENV=production
      {{if .U.github_client_id}}- GITHUB_CLIENT_ID={{ .U.github_client_id }}{{end}}
      - SYSTEM_DEBUG_LOGS=false
`,
	}
	for name, contents := range files {
		filename := filepath.Join(root, name)
		if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath := filepath.Join(root, "lzc-manifest.yml")
	if _, err := manifestedit.Apply(manifestPath, []manifestedit.Update{{
		Target:    manifestedit.Target{ID: "api", Kind: manifestedit.TargetService, Service: "api"},
		SourceRef: "ghcr.io/dockers-x/openship-api:0.4.5", RuntimeRef: "registry.lazycat.cloud/czyt/dockers-x/openship-api:new",
	}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"# upstream: ghcr.io/dockers-x/openship-api:0.4.5",
		"image: registry.lazycat.cloud/czyt/dockers-x/openship-api:new",
		"      {{if .U.github_client_id}}- GITHUB_CLIENT_ID={{ .U.github_client_id }}{{end}}",
	} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("updated manifest missing %q:\n%s", expected, data)
		}
	}
	if _, err := toolkitbuild.BuildFile(context.Background(), filepath.Join(root, "out.lpk"), toolkitbuild.Request{
		Root: root, ConfigFile: "lzc-build.yml", RunBuildScript: false,
	}); err != nil {
		t.Fatal(err)
	}
}
