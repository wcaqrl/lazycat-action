package lpkcheck_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actionbuild "github.com/wcaqrl/lazycat-action/internal/build"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/project"
)

func TestFileValidatesBuiltLPK(t *testing.T) {
	info := fixtureProject(t)
	built, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: info, Version: "1.2.3", Tag: "v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := lpkcheck.File(context.Background(), lpkcheck.Request{
		ProjectRoot: info.Root, Path: built.Path, ExpectedPackageID: info.PackageID, ExpectedVersion: info.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PackageID != info.PackageID || result.Version != "1.2.3" || result.SHA256 != built.SHA256 || result.Size == 0 || result.TargetPlatform != "linux/amd64" {
		t.Fatalf("result=%#v built=%#v", result, built)
	}
}

func TestFileReportsConfiguredARM64Target(t *testing.T) {
	info := fixtureProject(t)
	built, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: info, Version: "1.2.3", Tag: "v1.2.3", Target: platform.Target{OS: "linux", Arch: "arm64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := lpkcheck.File(context.Background(), lpkcheck.Request{
		ProjectRoot: info.Root, Path: built.Path, ExpectedPackageID: info.PackageID, ExpectedVersion: info.Version,
		Target: platform.Target{OS: "linux", Arch: "arm64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetPlatform != "linux/arm64" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFileValidatesBuiltLPKWithTemplateControls(t *testing.T) {
	info := fixtureProject(t)
	manifest := `application:
  subdomain: fixture
{{- if .U.multi_instance }}
  multi_instance: true
{{- end }}
  routes:
    - /=file:///lzcapp/pkg/content
`
	if err := os.WriteFile(info.ManifestFile, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	built, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: info, Version: "1.2.3", Tag: "v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := lpkcheck.File(context.Background(), lpkcheck.Request{
		ProjectRoot: info.Root, Path: built.Path, ExpectedPackageID: info.PackageID, ExpectedVersion: info.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PackageID != info.PackageID || result.Version != info.Version || result.SHA256 != built.SHA256 {
		t.Fatalf("result=%#v built=%#v", result, built)
	}
}

func TestFileRejectsUntrustedArtifactInputs(t *testing.T) {
	info := fixtureProject(t)
	built, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{Project: info, Version: "1.2.3", Tag: "v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		request lpkcheck.Request
		wantErr string
	}{
		{name: "package mismatch", request: lpkcheck.Request{ProjectRoot: info.Root, Path: built.Path, ExpectedPackageID: "cloud.lazycat.other", ExpectedVersion: "1.2.3"}, wantErr: "package"},
		{name: "version mismatch", request: lpkcheck.Request{ProjectRoot: info.Root, Path: built.Path, ExpectedPackageID: info.PackageID, ExpectedVersion: "1.2.4"}, wantErr: "version"},
	}
	outside := filepath.Join(t.TempDir(), "outside.lpk")
	data, err := os.ReadFile(built.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		t.Fatal(err)
	}
	tests = append(tests, struct {
		name    string
		request lpkcheck.Request
		wantErr string
	}{name: "outside root", request: lpkcheck.Request{ProjectRoot: info.Root, Path: outside, ExpectedPackageID: info.PackageID, ExpectedVersion: info.Version}, wantErr: "project root"})
	invalid := filepath.Join(info.Root, "invalid.lpk")
	if err := os.WriteFile(invalid, []byte("not an lpk"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests = append(tests, struct {
		name    string
		request lpkcheck.Request
		wantErr string
	}{name: "invalid archive", request: lpkcheck.Request{ProjectRoot: info.Root, Path: invalid, ExpectedPackageID: info.PackageID, ExpectedVersion: info.Version}, wantErr: "LPK"})
	link := filepath.Join(info.Root, "linked.lpk")
	if err := os.Symlink(built.Path, link); err != nil {
		t.Fatal(err)
	}
	tests = append(tests, struct {
		name    string
		request lpkcheck.Request
		wantErr string
	}{name: "symbolic link", request: lpkcheck.Request{ProjectRoot: info.Root, Path: link, ExpectedPackageID: info.PackageID, ExpectedVersion: info.Version}, wantErr: "symbolic link"})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := lpkcheck.File(context.Background(), test.request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestFileHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := lpkcheck.File(ctx, lpkcheck.Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func fixtureProject(t *testing.T) project.Info {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join("..", "..", "testdata", "static-app")
	for _, name := range []string{"package.yml", "lzc-build.yml", "lzc-manifest.yml"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content", "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	return project.Info{
		Root: root, BuildConfig: filepath.Join(root, "lzc-build.yml"), PackageFile: filepath.Join(root, "package.yml"),
		ManifestFile: filepath.Join(root, "lzc-manifest.yml"), Output: filepath.Join(root, "dist", "app.lpk"),
		PackageID: "cloud.lazycat.action.fixture", Version: "1.2.3", Kind: project.KindStatic,
	}
}
