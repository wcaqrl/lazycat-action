package project_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/project"
)

func TestInspectClassifiesProjectWithoutGuessingMainService(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     project.Kind
	}{
		{
			name: "static",
			manifest: `application:
  routes:
    - /=file:///lzcapp/pkg/content
`,
			want: project.KindStatic,
		},
		{
			name: "exec",
			manifest: `application:
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
`,
			want: project.KindExec,
		},
		{
			name: "exec upstream launch command",
			manifest: `application:
  upstreams:
    - location: /
      backend: http://127.0.0.1:8080/
      backend_launch_command: /lzcapp/pkg/content/app
`,
			want: project.KindExec,
		},
		{
			name: "service wins over routes",
			manifest: `application:
  routes:
    - /=file:///lzcapp/pkg/content
services:
  db:
    image: postgres:17
`,
			want: project.KindService,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := createProject(t, test.manifest)
			got, err := project.Inspect(context.Background(), config.Project{
				Root:        root,
				BuildConfig: "lzc-build.yml",
				PackageFile: "package.yml",
				Output:      "dist/app.lpk",
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != test.want {
				t.Fatalf("kind=%q want=%q", got.Kind, test.want)
			}
			if got.PackageID != "cloud.lazycat.example" || got.Version != "1.0.0" || got.Name != "Example" || got.Description != "Example summary" {
				t.Fatalf("info=%#v", got)
			}
			if !filepath.IsAbs(got.Output) || filepath.Base(got.ManifestFile) != "lzc-manifest.yml" {
				t.Fatalf("paths=%#v", got)
			}
		})
	}
}

func TestInspectHonorsManifestPathFromBuildConfig(t *testing.T) {
	root := createProject(t, "application: {}\n")
	if err := os.WriteFile(filepath.Join(root, "lzc-build.yml"), []byte("manifest: manifests/release.yml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "release.yml"), []byte("application: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := project.Inspect(context.Background(), config.Project{
		Root: root, BuildConfig: "lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(got.ManifestFile) != filepath.ToSlash(filepath.Join(root, "manifests", "release.yml")) {
		t.Fatalf("manifest=%q", got.ManifestFile)
	}
}

func TestInspectSupportsTemplateControlsInManifest(t *testing.T) {
	root := createProject(t, `application:
  subdomain: templated
{{- if .U.multi_instance }}
  multi_instance: true
{{- end }}
services:
  app:
    image: registry.lazycat.cloud/example/app:old
`)

	got, err := project.Inspect(context.Background(), config.Project{
		Root: root, BuildConfig: "lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != project.KindService || got.PackageID != "cloud.lazycat.example" || got.Version != "1.0.0" {
		t.Fatalf("info=%#v", got)
	}
}

func TestInspectFixedLazyCatContribFixtures(t *testing.T) {
	tests := []struct {
		name      string
		kind      project.Kind
		packageID string
	}{
		{name: "contrib-multiservice", kind: project.KindService, packageID: "community.lazycat.app.new-api"},
		{name: "contrib-exec", kind: project.KindExec, packageID: "cloud.lazycat.app.czyt.lazycat-mcp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.Abs(filepath.Join("..", "..", "testdata", test.name))
			if err != nil {
				t.Fatal(err)
			}
			got, err := project.Inspect(context.Background(), config.Project{
				Root: root, BuildConfig: "lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk",
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != test.kind || got.PackageID != test.packageID || got.Name == "" || got.Description == "" {
				t.Fatalf("info=%#v", got)
			}
		})
	}
}

func TestInspectRejectsEscapingProjectPath(t *testing.T) {
	root := createProject(t, "application: {}\n")
	_, err := project.Inspect(context.Background(), config.Project{
		Root: root, BuildConfig: "../lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk",
	})
	if err == nil {
		t.Fatal("expected escaping build config to fail")
	}
}

func TestInspectRejectsSymlinkedProjectInputs(t *testing.T) {
	root := createProject(t, "application: {}\n")
	external := filepath.Join(t.TempDir(), "external-build.yml")
	if err := os.WriteFile(external, []byte("contentdir: content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "lzc-build.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "lzc-build.yml")); err != nil {
		t.Fatal(err)
	}
	_, err := project.Inspect(context.Background(), config.Project{
		Root: root, BuildConfig: "lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk",
	})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err=%v", err)
	}
}

func createProject(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"package.yml":      "package: cloud.lazycat.example\nversion: 1.0.0\nname: Example\ndescription: Example summary\n",
		"lzc-build.yml":    "contentdir: content\n",
		"lzc-manifest.yml": manifest,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}
