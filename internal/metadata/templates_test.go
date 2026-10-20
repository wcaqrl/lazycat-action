package metadata_test

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/lpk"
	actionbuild "github.com/wcaqrl/lazycat-action/internal/build"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/yamledit"
)

// Exercise the distributed assets with the real config/manifest/build code.
// Image delivery is a local fixture: no registry or store is contacted, and
// these archive tests deliberately do not claim upstream application health.
func TestAdapterTemplatesProduceVerifiedOfficialLPKs(t *testing.T) {
	assets := filepath.Join("..", "..", "skills", "lazycat-github-action", "assets")
	for _, recipe := range []string{"starter", "oci", "custom-image", "source-build", "multi-image"} {
		t.Run(recipe, func(t *testing.T) {
			root := t.TempDir()
			copyTemplate(t, filepath.Join(assets, "adapter"), root)
			switch recipe {
			case "starter":
			case "multi-image":
				copyTemplate(t, filepath.Join(assets, "recipes", "multi-image"), root)
			default:
				copyTemplateFile(t, filepath.Join(assets, "recipes", recipe+".yml"), filepath.Join(root, "lazycat-action.yml"))
				if recipe == "custom-image" {
					copyTemplate(t, filepath.Join(assets, "recipes", "custom-image"), filepath.Join(root, "image"))
				}
			}

			cfg, err := config.Load(filepath.Join(root, "lazycat-action.yml"))
			if err != nil {
				t.Fatal(err)
			}
			cfg.Project.Root = root
			info, err := project.Inspect(context.Background(), cfg.Project)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, ".github", "workflows", "update.yml")); err != nil {
				t.Fatalf("starter is missing its hidden workflow: %v", err)
			}
			updates := make([]manifestedit.Update, 0, len(cfg.Images))
			for _, binding := range cfg.Images {
				target := manifestedit.Target{ID: binding.ID, Kind: manifestedit.TargetService, Service: binding.Service}
				// Read fails if a recipe names a nonexistent Manifest service.
				if _, err := manifestedit.Read(info.ManifestFile, []manifestedit.Target{target}); err != nil {
					t.Fatal(err)
				}
				updates = append(updates, manifestedit.Update{
					Target: target, SourceRef: "ghcr.io/example/" + binding.ID + ":1.0.0@sha256:" + strings.Repeat("a", 64),
					RuntimeRef: "registry.lazycat.cloud/example/" + binding.ID + ":fixture",
				})
			}
			if _, err := manifestedit.Apply(info.ManifestFile, updates); err != nil {
				t.Fatal(err)
			}
			if _, err := yamledit.SetPackageVersion(info.PackageFile, "1.0.0"); err != nil {
				t.Fatal(err)
			}
			info, err = project.Inspect(context.Background(), cfg.Project)
			if err != nil {
				t.Fatal(err)
			}
			built, err := (actionbuild.Builder{Stdout: io.Discard, Stderr: io.Discard}).Build(context.Background(), actionbuild.Request{
				Project: info, Version: "1.0.0", Official: true, FailOnWarnings: true, Target: cfg.Project.Target(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(built.SHA256) != 64 || built.Size == 0 {
				t.Fatalf("invalid build result: %#v", built)
			}
			reader, err := lpk.OpenFile(context.Background(), built.Path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			read := func(name string) []byte {
				t.Helper()
				entry, err := reader.OpenEntry(context.Background(), name)
				if err != nil {
					t.Fatal(err)
				}
				defer entry.Close()
				data, err := io.ReadAll(entry)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			icon := read("icon.png")
			image, err := png.DecodeConfig(bytes.NewReader(icon))
			if err != nil || image.Width != 512 || image.Height != 512 {
				t.Fatalf("invalid packaged PNG: %#v, %v", image, err)
			}
			manifest := read("manifest.yml")
			if !bytes.Contains(manifest, []byte("{{ .S.AppDomain }}")) ||
				!bytes.Contains(manifest, []byte("/lzcapp/var/data:/data")) {
				t.Fatal("domain template or persistence bind was lost during packaging")
			}
			if recipe == "multi-image" {
				if !bytes.Contains(manifest, []byte("{{ .U.database_password }}")) ||
					!bytes.Contains(read("deploy_params.yml"), []byte("id: database_password")) {
					t.Fatal("installation parameter was not preserved or collected")
				}
			}
			effective, err := reader.EffectiveManifest(context.Background())
			if err != nil || effective.Manifest.Version != "1.0.0" || effective.Manifest.Package != "cloud.lazycat.app.example" {
				t.Fatalf("packaged metadata mismatch: %#v, %v", effective, err)
			}
		})
	}
}

func TestLegacyAssetPathsMatchCompleteStarter(t *testing.T) {
	assets := filepath.Join("..", "..", "skills", "lazycat-github-action", "assets")
	for old, current := range map[string]string{
		"lazycat-action.yml":   "adapter/lazycat-action.yml",
		"lazycat-workflow.yml": "adapter/.github/workflows/update.yml",
	} {
		oldBytes, err := os.ReadFile(filepath.Join(assets, old))
		if err != nil {
			t.Fatal(err)
		}
		currentBytes, err := os.ReadFile(filepath.Join(assets, current))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(oldBytes, currentBytes) {
			t.Fatalf("legacy asset %s drifted from %s", old, current)
		}
	}
}

func copyTemplate(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		copyTemplateFile(t, path, target)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func copyTemplateFile(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
