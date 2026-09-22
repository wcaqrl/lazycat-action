package action_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/wcaqrl/lazycat-action/internal/action"
	actionbuild "github.com/wcaqrl/lazycat-action/internal/build"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/delivery"
	"github.com/wcaqrl/lazycat-action/internal/imageflow"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
	"github.com/wcaqrl/lazycat-action/internal/pipelinestate"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/prepare"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/publishflow"
	"github.com/wcaqrl/lazycat-action/internal/registry"
	"github.com/wcaqrl/lazycat-action/internal/source"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	"github.com/wcaqrl/lazycat-action/internal/yamledit"
)

func TestRunBuildCallsDependenciesInOrderAndReturnsStableResult(t *testing.T) {
	var calls []string
	root := t.TempDir()
	packageFile := filepath.Join(root, "package.yml")
	manifestFile := filepath.Join(root, "lzc-manifest.yml")
	deps := action.Dependencies{
		Host:      platform.Host{OS: "linux", Arch: "arm64"},
		ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) {
			calls = append(calls, "load")
			return gitConfig(), nil
		},
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			calls = append(calls, "inspect")
			version := "1.0.0"
			if len(calls) > 2 {
				version = "1.2.3"
			}
			return project.Info{Root: root, PackageFile: packageFile, ManifestFile: manifestFile, Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(filename, version string) (yamledit.Change, error) {
			calls = append(calls, "edit")
			if filename != packageFile || version != "1.2.3" {
				t.Fatalf("edit filename=%q version=%q", filename, version)
			}
			return yamledit.Change{Changed: true, Old: "1.0.0", New: "1.2.3"}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			calls = append(calls, "build")
			if request.Version != "1.2.3" || request.Project.Version != "1.2.3" || request.Project.Output == "" {
				t.Fatalf("request=%#v", request)
			}
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
	}

	result, err := action.Run(context.Background(), action.Input{
		Operation: action.OperationBuild, ConfigPath: ".github/lazycat-action.yml", Version: "1.2.3", Tag: "v1.2.3",
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"load", "inspect", "edit", "inspect", "build"}) {
		t.Fatalf("calls=%v", calls)
	}
	if result.Operation != "build" || !result.Changed || result.PackageID != "cloud.lazycat.example" || result.Version != "1.2.3" || result.Tag != "v1.2.3" {
		t.Fatalf("result=%#v", result)
	}
	if result.PackageFile != packageFile || result.ManifestFile != manifestFile {
		t.Fatalf("managed files=%#v", result)
	}
	if result.RunnerArch != "arm64" || result.TargetPlatform != "linux/amd64" || string(result.ImageResults) != "[]" {
		t.Fatalf("architectures/result=%#v", result)
	}
	if result.OfficialStoreEnabled || string(result.StoreResults) != "{}" {
		t.Fatalf("store result=%#v", result)
	}
	if result.ResultFile == "" {
		t.Fatal("result file is empty")
	}
	if _, err := os.Stat(result.ResultFile); err != nil {
		t.Fatal(err)
	}
}

func TestRunVersion2BranchSourcePackagesAndPersistsResumableState(t *testing.T) {
	root := t.TempDir()
	packageFile := filepath.Join(root, "package.yml")
	manifestFile := filepath.Join(root, "lzc-manifest.yml")
	cfg := config.Config{
		Version:   2,
		Project:   config.Project{Root: root, Output: "dist/app.lpk", TargetArch: "amd64"},
		Source:    config.Source{Kind: config.SourceKindGit, URL: "git@gitee.com:acme/app.git", Select: config.SourceSelect{Strategy: "branch-head", Branch: "auto"}},
		Changelog: config.Changelog{GitURL: "git@gitee.com:acme/app.git", AuthRef: "source"},
		State:     config.State{File: ".lazycat-action.lock.yml"},
		Update:    config.Update{Strategy: config.StrategyPublish},
		Build:     config.Build{Prepare: config.Prepare{Mode: "command", Command: "./scripts/build.sh"}},
		Stores:    config.Stores{Official: config.OfficialStore{Enabled: true}},
	}
	written := pipelinestate.Lock{}
	inspectCalls := 0
	deps := action.Dependencies{
		Host: platform.Host{OS: "linux", Arch: "amd64"}, ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCalls++
			version := "1.0.0"
			if inspectCalls > 1 {
				version = "1.0.1"
			}
			return project.Info{Root: root, PackageFile: packageFile, ManifestFile: manifestFile, Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			if version != "1.0.1" {
				t.Fatalf("version=%q", version)
			}
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
		DiscoverSource: func(context.Context, source.Request) (source.Candidate, error) {
			return source.Candidate{Kind: "git", Ref: "refs/heads/master", Branch: "master", Revision: strings.Repeat("b", 40)}, nil
		},
		DiscoverChangelog: func(_ context.Context, settings config.Changelog, _, _ source.Candidate, previousVersion, nextVersion string) (string, error) {
			if settings.AuthRef != "source" || previousVersion != "1.0.0" || nextVersion != "1.0.1" {
				t.Fatalf("settings=%#v previous=%q next=%q", settings, previousVersion, nextVersion)
			}
			return "Upstream 1.0.1\n- Fix login", nil
		},
		Fingerprint: func(context.Context, config.Config, source.Candidate) (string, error) {
			return "sha256:fingerprint", nil
		},
		ReadState:  func(string) (pipelinestate.Lock, error) { return pipelinestate.Lock{}, nil },
		WriteState: func(_ string, lock pipelinestate.Lock) error { written = lock; return nil },
		PrepareSource: func(_ context.Context, request prepare.Request) (prepare.Result, error) {
			if request.ApplicationVersion != "1.0.1" {
				t.Fatalf("application version=%q", request.ApplicationVersion)
			}
			return prepare.Result{}, nil
		},
	}
	result, err := action.Run(t.Context(), action.Input{Operation: action.OperationCheck, EventName: "schedule"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "1.0.1" || result.Fingerprint != "sha256:fingerprint" || result.LPKPath == "" || !strings.Contains(result.Changelog, "Fix login") {
		t.Fatalf("result=%#v", result)
	}
	if written.Status != "packaged" || written.Application.Version != "1.0.1" || written.Source.Branch != "master" || written.Application.Changelog != result.Changelog {
		t.Fatalf("state=%#v", written)
	}
}

func TestRunVersion2PinsPreparedImageBeforeOfficialCopy(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:" + strings.Repeat("a", 64)
	cfg := config.Config{
		Version: 2,
		Project: config.Project{Root: root, Output: "dist/app.lpk", TargetArch: "amd64"},
		Source:  config.Source{Kind: config.SourceKindGit, URL: "https://github.com/acme/app.git"},
		State:   config.State{File: ".lazycat-action.lock.yml"},
		Update:  config.Update{Strategy: config.StrategyPublish},
		Build:   config.Build{Prepare: config.Prepare{Mode: "command", Command: "./build.sh", Output: "ttl.sh/acme/app:{version}"}},
		Images: []config.Image{{
			ID: "runtime", Target: "service", Service: "app",
			Delivery: config.Delivery{Mode: "lazycat"},
		}},
	}
	var deliveryRequest delivery.Request
	var manifestUpdate manifestedit.Update
	inspectCalls := 0
	deps := action.Dependencies{
		Host: platform.Host{OS: "linux", Arch: "amd64"}, ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCalls++
			version := "1.0.0"
			if inspectCalls > 1 {
				version = "1.0.1"
			}
			return project.Info{
				Root: root, PackageFile: filepath.Join(root, "package.yml"), ManifestFile: filepath.Join(root, "lzc-manifest.yml"),
				Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: version,
			}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) {
			return yamledit.Change{Changed: true, Old: "1.0.0", New: "1.0.1"}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("b", 64), TargetPlatform: "linux/amd64"}, nil
		},
		DiscoverSource: func(context.Context, source.Request) (source.Candidate, error) {
			return source.Candidate{Kind: "git", Ref: "refs/heads/main", Branch: "main", Revision: strings.Repeat("c", 40)}, nil
		},
		Fingerprint: func(context.Context, config.Config, source.Candidate) (string, error) {
			return "sha256:fingerprint", nil
		},
		ReadState:  func(string) (pipelinestate.Lock, error) { return pipelinestate.Lock{}, nil },
		WriteState: func(string, pipelinestate.Lock) error { return nil },
		PrepareSource: func(context.Context, prepare.Request) (prepare.Result, error) {
			return prepare.Result{Image: "ttl.sh/acme/app:1.0.1"}, nil
		},
		ReadManifestImages: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "runtime", RuntimeRef: "registry.lazycat.cloud/acme/old:1.0.0"}}, nil
		},
		InspectPreparedImage: func(context.Context, string, platform.Target) (registry.Image, error) {
			return registry.Image{Digest: digest, Platform: "linux/amd64"}, nil
		},
		DeliverPreparedImage: func(_ context.Context, request delivery.Request) (delivery.Result, error) {
			deliveryRequest = request
			return delivery.Result{RuntimeRef: "registry.lazycat.cloud/acme/app:1.0.1", Copied: true}, nil
		},
		ApplyManifestImages: func(_ string, updates []manifestedit.Update) ([]manifestedit.Change, error) {
			manifestUpdate = updates[0]
			return []manifestedit.Change{{Changed: true}}, nil
		},
	}
	if _, err := action.Run(t.Context(), action.Input{Operation: action.OperationCheck}, deps); err != nil {
		t.Fatal(err)
	}
	want := "ttl.sh/acme/app:1.0.1@" + digest
	if deliveryRequest.SourceRef != want || manifestUpdate.SourceRef != want {
		t.Fatalf("delivery source=%q manifest source=%q want=%q", deliveryRequest.SourceRef, manifestUpdate.SourceRef, want)
	}
}

func TestRunBuildKeepsOfficialWarningsStoreScoped(t *testing.T) {
	tests := []struct {
		name         string
		official     bool
		wantOfficial bool
	}{
		{name: "official enabled", official: true, wantOfficial: true},
		{name: "official disabled", official: false, wantOfficial: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := gitConfig()
			cfg.Stores.Official.Enabled = test.official
			var buildRequest actionbuild.Request
			deps := action.Dependencies{
				Host:       platform.Host{OS: "linux", Arch: "amd64"},
				ResultDir:  filepath.Join(root, "results"),
				LoadConfig: func(string) (config.Config, error) { return cfg, nil },
				Inspect: func(context.Context, config.Project) (project.Info, error) {
					return project.Info{
						Root: root, PackageFile: filepath.Join(root, "package.yml"), Output: filepath.Join(root, "dist", "app.lpk"),
						PackageID: "cloud.lazycat.example", Version: "1.2.3",
					}, nil
				},
				SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
				Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
					buildRequest = request
					return actionbuild.Result{
						Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version,
						SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64",
						Warnings: []lpkgo.Warning{{Code: "unknown-manifest-fields", Path: "services.web.container_name"}},
					}, nil
				},
			}

			result, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
			if err != nil {
				t.Fatal(err)
			}
			if buildRequest.Official != test.wantOfficial || buildRequest.FailOnWarnings {
				t.Fatalf("build request=%#v", buildRequest)
			}
			if len(result.Warnings) != 1 || result.Warnings[0].Path != "services.web.container_name" {
				t.Fatalf("warnings=%#v", result.Warnings)
			}
		})
	}
}

func TestRunPublishesOfficialStoreAndReturnsStableJSON(t *testing.T) {
	root := t.TempDir()
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Stores.Official.Enabled = true
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.2.3", Name: "Example"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		Publish: func(_ context.Context, request publishflow.Request) (publishflow.Result, error) {
			if request.LPKPath != filepath.Join(root, "dist", "app.lpk") {
				t.Fatalf("request=%#v", request)
			}
			return publishflow.Result{
				Artifact: lpkcheckResult(filepath.Join(root, "dist", "app.lpk")),
				Official: &official.Result{Published: false, Skipped: true, OnlineVersion: "1.2.3", PackageID: "cloud.lazycat.example", Version: "1.2.3", SHA256: strings.Repeat("a", 64)},
			}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{
		Operation: action.OperationPublishOfficial, Version: "1.2.3", LPKPath: filepath.Join(root, "dist", "app.lpk"),
		Changelog: "Release notes",
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != "publish-official" || result.SHA256 != strings.Repeat("a", 64) || !result.OfficialStoreEnabled || string(result.StoreResults) == "{}" || !strings.Contains(string(result.StoreResults), `"skipped":true`) || !strings.Contains(string(result.StoreResults), `"onlineVersion":"1.2.3"`) {
		t.Fatalf("result=%#v", result)
	}
}

func TestRunPublishUsesSavedChangelogWhenRetryHasNoInput(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{Version: 2, Project: config.Project{Root: root}, State: config.State{File: ".lazycat-action.lock.yml"}, Update: config.Update{Strategy: config.StrategyPublish}, Stores: config.Stores{Official: config.OfficialStore{Enabled: true}}}
	lock := pipelinestate.Lock{Fingerprint: "sha256:example", Status: "packaged", Application: pipelinestate.Application{Version: "1.2.3", Changelog: "Upstream 1.2.3\n- Fix login"}}
	deps := action.Dependencies{
		Host: platform.Host{OS: "linux", Arch: "amd64"}, ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.2.3"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		ReadState:  func(string) (pipelinestate.Lock, error) { return lock, nil },
		WriteState: func(_ string, saved pipelinestate.Lock) error { lock = saved; return nil },
		Publish: func(_ context.Context, request publishflow.Request) (publishflow.Result, error) {
			if request.Changelog != "Upstream 1.2.3\n- Fix login" {
				t.Fatalf("changelog=%q", request.Changelog)
			}
			return publishflow.Result{Artifact: lpkcheckResult(filepath.Join(root, "dist", "app.lpk"))}, nil
		},
	}
	result, err := action.Run(t.Context(), action.Input{Operation: action.OperationPublishOfficial, Version: "1.2.3", LPKPath: filepath.Join(root, "dist", "app.lpk")}, deps)
	if err != nil || result.Changelog != lock.Application.Changelog || lock.Status != "submitted" {
		t.Fatalf("result=%#v lock=%#v err=%v", result, lock, err)
	}
}

func TestRunGuardedOfficialPublishReturnsPausedResultAfterFinalReviewRecheck(t *testing.T) {
	root := t.TempDir()
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Stores.Official.Enabled = true
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.2.3"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		Publish: func(context.Context, publishflow.Request) (publishflow.Result, error) {
			return publishflow.Result{}, fmt.Errorf("publish official store: %w", &official.PendingReviewError{Version: "1.2.3"})
		},
	}
	result, err := action.Run(t.Context(), action.Input{
		Operation: action.OperationPublishOfficial, Version: "1.2.3", LPKPath: filepath.Join(root, "app.lpk"),
		Changelog: "Release notes", GuardOfficialReview: true,
	}, deps)
	if err != nil || !result.OfficialReviewPending || result.OfficialReviewVersion != "1.2.3" || result.Version != "1.2.3" || result.Operation != "publish-official" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRunMapsStoreAuthenticationFailure(t *testing.T) {
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Stores.Official.Enabled = true
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: t.TempDir(), PackageID: "cloud.lazycat.example", Version: "1.2.3"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		Publish: func(context.Context, publishflow.Request) (publishflow.Result, error) {
			return publishflow.Result{}, &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Retryable: false}
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationPublishOfficial, Version: "1.2.3", LPKPath: "dist/app.lpk"}, deps)
	var actionErr *action.Error
	if !errors.As(err, &actionErr) || actionErr.Code != action.CodeStoreAuthFailed {
		t.Fatalf("err=%#v", err)
	}
}

func TestErrorIncludesSafeToolkitDiagnosticsWithoutCauseText(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeStorePublishFailed,
		Message: "store publishing failed",
		Cause: &lpkgo.Error{
			Code: lpkgo.CodeConflict, Op: "store.official", StatusCode: 409,
			Cause: errors.New("response contains lcst_must_not_leak"),
		},
	}
	message := err.Error()
	for _, expected := range []string{"STORE_PUBLISH_FAILED", "CONFLICT", "status=409", "op=store.official"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message=%q missing %q", message, expected)
		}
	}
	if strings.Contains(message, "lcst_must_not_leak") {
		t.Fatalf("message leaked cause text: %q", message)
	}
}

func TestErrorIncludesExplicitPublicUpstreamMessage(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeStorePublishFailed,
		Message: "store publishing failed",
		Cause: &lpkgo.Error{
			Code: lpkgo.CodeRemoteUnavailable, Op: "store.official.review", StatusCode: 400,
			Cause: publicDetailError{message: "version already pending review"},
		},
	}
	message := err.Error()
	if !strings.Contains(message, `message="version already pending review"`) {
		t.Fatalf("message=%q", message)
	}
}

func TestErrorIncludesExplicitPublicRegistryMessage(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeConfigInvalid,
		Message: "image check failed",
		Cause:   publicDetailError{message: "image repository returned 10347 tags; raw limit is 10000"},
	}
	message := err.Error()
	if !strings.Contains(message, `message="image repository returned 10347 tags; raw limit is 10000"`) {
		t.Fatalf("message=%q", message)
	}
}

func TestErrorIncludesSafeBuildPathLineAndMessage(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeBuildFailed,
		Message: "LPK validation build failed after image update",
		Cause: &lpkgo.Error{
			Code: lpkgo.CodeInvalidConfig,
			Op:   "build.template_manifest",
			Path: "/tmp/actions-runner/_work/openship/lzc-manifest.yml",
			Cause: publicDetailError{
				message: "yaml:\n  line 90: block sequence entries are not allowed in this context",
				path:    "/tmp/actions-runner/_work/openship/lzc-manifest.yml",
			},
		},
	}
	message := err.Error()
	for _, expected := range []string{
		"BUILD_FAILED: LPK validation build failed after image update",
		"upstream=INVALID_CONFIG",
		"op=build.template_manifest",
		"path=lzc-manifest.yml",
		`message="yaml: line 90: block sequence entries are not allowed in this context"`,
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message=%q missing %q", message, expected)
		}
	}
	if strings.Contains(message, "/tmp/actions-runner") {
		t.Fatalf("message leaked runner path: %q", message)
	}
}

func TestErrorKeepsSafeYAMLProblemContainingWordToken(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeBuildFailed,
		Message: "LPK validation build failed",
		Cause: &lpkgo.Error{
			Code:  lpkgo.CodeInvalidConfig,
			Cause: publicDetailError{message: "yaml: line 4: found character that cannot start any token"},
		},
	}
	if message := err.Error(); !strings.Contains(message, `message="yaml: line 4: found character that cannot start any token"`) {
		t.Fatalf("message=%q", message)
	}
}

func TestErrorNormalizesWindowsBuildPath(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeBuildFailed,
		Message: "LPK validation build failed",
		Cause: &lpkgo.Error{
			Code:  lpkgo.CodeInvalidConfig,
			Path:  `C:\actions-runner\_work\openship\lzc-manifest.yml`,
			Cause: publicDetailError{path: `C:\actions-runner\_work\openship\lzc-manifest.yml`},
		},
	}
	message := err.Error()
	if !strings.Contains(message, "path=lzc-manifest.yml") || strings.Contains(message, "actions-runner") {
		t.Fatalf("message=%q", message)
	}
}

func TestErrorDoesNotExposeExpandedBuildConfigSecret(t *testing.T) {
	const secret = "ghs_must_not_leak"
	t.Setenv("GITHUB_TOKEN", secret)
	root := t.TempDir()
	for name, contents := range map[string]string{
		"lzc-build.yml":    "manifest: ./lzc-manifest.yml\nenvs: ${GITHUB_TOKEN}\n",
		"package.yml":      "package: cloud.lazycat.action.secret-fixture\nversion: 1.2.3\nname: Secret Fixture\n",
		"lzc-manifest.yml": "application:\n  subdomain: secret-fixture\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, buildErr := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: project.Info{
			Root: root, BuildConfig: filepath.Join(root, "lzc-build.yml"), PackageFile: filepath.Join(root, "package.yml"),
			ManifestFile: filepath.Join(root, "lzc-manifest.yml"), Output: filepath.Join(root, "dist", "app.lpk"),
			PackageID: "cloud.lazycat.action.secret-fixture", Version: "1.2.3", Kind: project.KindStatic,
		},
		Version: "1.2.3", Tag: "v1.2.3",
	})
	if buildErr == nil {
		t.Fatal("expected invalid expanded build config to fail")
	}
	message := (&action.Error{Code: action.CodeBuildFailed, Message: "LPK validation build failed", Cause: buildErr}).Error()
	for _, forbidden := range []string{secret, "ghs_", "cannot unmarshal", "/tmp/"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("message leaked %q: %q", forbidden, message)
		}
	}
	for _, expected := range []string{"upstream=INVALID_CONFIG", "path=lzc-build.yml"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message=%q missing %q", message, expected)
		}
	}
}

func TestErrorDoesNotExposeEnvironmentExpandedOpaqueCredentialPath(t *testing.T) {
	const opaqueCredential = "AKIAIOSFODNN7EXAMPLE"
	t.Setenv("AWS_SECRET_ACCESS_KEY", opaqueCredential)
	root := t.TempDir()
	for name, contents := range map[string]string{
		"lzc-build.yml":    "manifest: ${AWS_SECRET_ACCESS_KEY}\n",
		"package.yml":      "package: cloud.lazycat.action.opaque-path\nversion: 1.2.3\nname: Opaque Path\n",
		"lzc-manifest.yml": "application:\n  subdomain: opaque-path\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, buildErr := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: project.Info{
			Root: root, BuildConfig: filepath.Join(root, "lzc-build.yml"), PackageFile: filepath.Join(root, "package.yml"),
			ManifestFile: filepath.Join(root, "lzc-manifest.yml"), Output: filepath.Join(root, "dist", "app.lpk"),
			PackageID: "cloud.lazycat.action.opaque-path", Version: "1.2.3", Kind: project.KindStatic,
		},
		Version: "1.2.3", Tag: "v1.2.3",
	})
	if buildErr == nil {
		t.Fatal("expected environment-expanded manifest path to fail")
	}
	message := (&action.Error{Code: action.CodeBuildFailed, Message: "LPK validation build failed", Cause: buildErr}).Error()
	for _, forbidden := range []string{opaqueCredential, "AWS_SECRET_ACCESS_KEY", "path=", root} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("message leaked %q: %q", forbidden, message)
		}
	}
	for _, expected := range []string{"upstream=INVALID_CONFIG", "op=build.manifest"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message=%q missing %q", message, expected)
		}
	}
}

func TestErrorSuppressesSensitivePublicDiagnostics(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeBuildFailed,
		Message: "LPK validation build failed",
		Cause: &lpkgo.Error{
			Code:  lpkgo.CodeInvalidConfig,
			Op:    "build.template_manifest",
			Path:  "config/client_secret.yml",
			Cause: publicDetailError{path: "config/client_secret.yml", message: "authorization token=must-not-leak"},
		},
	}
	message := err.Error()
	if strings.Contains(message, "path=") || strings.Contains(message, "message=") || strings.Contains(message, "must-not-leak") {
		t.Fatalf("message leaked sensitive diagnostics: %q", message)
	}
}

func TestErrorBoundsPublicDetail(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeBuildFailed,
		Message: "LPK validation build failed",
		Cause: &lpkgo.Error{
			Code:  lpkgo.CodeInvalidManifest,
			Cause: publicDetailError{message: strings.Repeat("x", 600)},
		},
	}
	message := err.Error()
	want := `message="` + strings.Repeat("x", 512) + `"`
	if !strings.Contains(message, want) || strings.Contains(message, strings.Repeat("x", 513)) {
		t.Fatalf("message was not bounded to 512 bytes: %q", message)
	}
}

func TestErrorSuppressesTraversalAndUnicodeSeparatorPaths(t *testing.T) {
	for _, unsafePath := range []string{"../../runner/lzc-manifest.yml", "config/lzc-\u2028manifest.yml"} {
		err := &action.Error{
			Code:    action.CodeBuildFailed,
			Message: "LPK validation build failed",
			Cause: &lpkgo.Error{
				Code:  lpkgo.CodeInvalidManifest,
				Path:  unsafePath,
				Cause: publicDetailError{path: unsafePath},
			},
		}
		if message := err.Error(); strings.Contains(message, "path=") || strings.Contains(message, "runner") || strings.Contains(message, "\u2028") {
			t.Fatalf("message leaked unsafe path %q: %q", unsafePath, message)
		}
	}
}

type publicDetailError struct {
	message string
	path    string
}

func (err publicDetailError) Error() string {
	return "upstream response rejected"
}

func (err publicDetailError) PublicErrorDetail() string {
	return err.message
}

func (err publicDetailError) PublicErrorPath() string {
	return err.path
}

func lpkcheckResult(path string) lpkcheck.Result {
	return lpkcheck.Result{Path: path, PackageID: "cloud.lazycat.example", Version: "1.2.3", SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}
}

func TestRunDryRunSkipsEditsAndBuild(t *testing.T) {
	root := t.TempDir()
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) {
			t.Fatal("SetVersion called during dry-run")
			return yamledit.Change{}, nil
		},
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			t.Fatal("Build called during dry-run")
			return actionbuild.Result{}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3", Tag: "v1.2.3", DryRun: true}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.LPKPath != "" || result.TargetPlatform != "linux/amd64" {
		t.Fatalf("result=%#v", result)
	}
}

func TestRunRejectsSymlinkedResultDirectory(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	resultDir := filepath.Join(root, "results")
	if err := os.Symlink(external, resultDir); err != nil {
		t.Fatal(err)
	}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  resultDir,
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3", DryRun: true}, deps)
	if err == nil || !strings.Contains(err.Error(), action.CodeBuildFailed) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunRejectsWorkflowToolchainMismatchBeforeProjectInspection(t *testing.T) {
	inspected := false
	cfg := gitConfig()
	cfg.Build.Toolchains = []config.Toolchain{{Kind: "go", Version: "1.25.x"}, {Kind: "docker"}}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspected = true
			return project.Info{}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, WorkflowToolchains: "go,node", WorkflowGoVersion: "1.24.x"}, deps)
	if err == nil || !strings.Contains(err.Error(), "workflow toolchains do not match") {
		t.Fatalf("err=%v", err)
	}
	if inspected {
		t.Fatal("project inspection ran after toolchain mismatch")
	}
}

func TestRunBuildFailureRollsBackVersion(t *testing.T) {
	root := t.TempDir()
	var edits []string
	inspectCount := 0
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCount++
			version := "1.0.0"
			if inspectCount > 1 {
				version = "1.2.3"
			}
			return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			edits = append(edits, version)
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, errors.New("compile failed")
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
	if err == nil || !strings.Contains(err.Error(), action.CodeBuildFailed) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(edits, []string{"1.2.3", "1.0.0"}) {
		t.Fatalf("edits=%v", edits)
	}
}

func TestRunRejectsNonAMD64BuildResult(t *testing.T) {
	root := t.TempDir()
	inspectCount := 0
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "arm64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCount++
			version := "1.0.0"
			if inspectCount > 1 {
				version = "1.2.3"
			}
			return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{PackageID: "cloud.lazycat.example", Version: "1.2.3", TargetPlatform: "linux/arm64"}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
	if err == nil || !strings.Contains(err.Error(), action.CodeLPKInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunAcceptsConfiguredARM64BuildResult(t *testing.T) {
	root := t.TempDir()
	inspectCount := 0
	cfg := gitConfig()
	cfg.Project.TargetArch = "arm64"
	var buildRequest actionbuild.Request
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCount++
			version := "1.0.0"
			if inspectCount > 1 {
				version = "1.2.3"
			}
			return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			buildRequest = request
			return actionbuild.Result{PackageID: "cloud.lazycat.example", Version: "1.2.3", TargetPlatform: "linux/arm64"}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if buildRequest.Target.Platform() != "linux/arm64" || result.TargetPlatform != "linux/arm64" || result.RunnerArch != "amd64" {
		t.Fatalf("request=%#v result=%#v", buildRequest, result)
	}
}

func TestResolveOperation(t *testing.T) {
	git := gitConfig()
	image := gitConfig()
	image.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	v2 := gitConfig()
	v2.Version = 2
	v2.Update.VersionSource = config.VersionSource{}

	tests := []struct {
		name  string
		input action.Input
		cfg   config.Config
		want  action.Operation
	}{
		{name: "release", input: action.Input{Operation: action.OperationAuto, EventName: "release"}, cfg: git, want: action.OperationBuild},
		{name: "manual git build", input: action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch"}, cfg: git, want: action.OperationBuild},
		{name: "manual image check", input: action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch"}, cfg: image, want: action.OperationCheck},
		{name: "manual explicit version build", input: action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch", Version: "1.2.3"}, cfg: image, want: action.OperationBuild},
		{name: "tag git build", input: action.Input{Operation: action.OperationAuto, EventName: "push", RefType: "tag", RefName: "v1.2.3"}, cfg: git, want: action.OperationBuild},
		{name: "scheduled image check", input: action.Input{Operation: action.OperationAuto, EventName: "schedule"}, cfg: image, want: action.OperationCheck},
		{name: "explicit check unchanged", input: action.Input{Operation: action.OperationCheck, EventName: "workflow_dispatch"}, cfg: git, want: action.OperationCheck},
		{name: "explicit build unchanged", input: action.Input{Operation: action.OperationBuild, EventName: "schedule"}, cfg: image, want: action.OperationBuild},
		{name: "v2 cli auto check", input: action.Input{Operation: action.OperationAuto}, cfg: v2, want: action.OperationCheck},
		{name: "v2 branch push auto check", input: action.Input{Operation: action.OperationAuto, EventName: "push", RefName: "main"}, cfg: v2, want: action.OperationCheck},
		{name: "v2 explicit version auto build", input: action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch", Version: "2.0.0"}, cfg: v2, want: action.OperationBuild},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := action.ResolveOperation(test.input, test.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("operation=%q want=%q", got, test.want)
			}
		})
	}
}

func TestRunAutoPublishPausesBeforeImageWorkWhileOfficialReviewIsPending(t *testing.T) {
	tests := []struct {
		name          string
		packageID     string
		reviewVersion string
		latestVersion string
		found         bool
		wantChecks    int
		wantPaused    bool
	}{
		{name: "no review continues", packageID: "cloud.lazycat.no-review", latestVersion: "1.3.0", wantChecks: 1},
		{name: "pending review at latest pauses", packageID: "cloud.lazycat.pending-review", reviewVersion: "1.4.0", latestVersion: "1.4.0", found: true, wantChecks: 1, wantPaused: true},
		{name: "older pending review continues", packageID: "cloud.lazycat.older-review", reviewVersion: "1.2.0", latestVersion: "1.3.0", found: true, wantChecks: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := gitConfig()
			cfg.Update.Strategy = config.StrategyPublish
			cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
			cfg.Stores.Official.Enabled = true
			checks := 0
			deps := action.Dependencies{
				Host:       platform.Host{OS: "linux", Arch: "amd64"},
				ResultDir:  filepath.Join(root, "results"),
				LoadConfig: func(string) (config.Config, error) { return cfg, nil },
				Inspect: func(context.Context, config.Project) (project.Info, error) {
					return project.Info{Root: root, PackageID: test.packageID, Version: "1.3.0"}, nil
				},
				SetVersion: func(string, string) (yamledit.Change, error) {
					t.Fatal("unchanged image check must not edit package.yml")
					return yamledit.Change{}, nil
				},
				Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
					t.Fatal("waiting-review preflight must run before builds")
					return actionbuild.Result{}, nil
				},
				WaitingReviewVersion: func(_ context.Context, packageID string) (string, bool, error) {
					if packageID != test.packageID {
						t.Fatalf("package=%q", packageID)
					}
					return test.reviewVersion, test.found, nil
				},
				CheckImages: func(_ context.Context, request imageflow.Request) (imageflow.Result, error) {
					checks++
					if request.OfficialReviewVersion != "" && test.wantPaused {
						return imageflow.Result{}, fmt.Errorf("%w: review %s covers candidate %s", imageflow.ErrOfficialReviewCoversCandidate, request.OfficialReviewVersion, test.latestVersion)
					}
					return imageflow.Result{Version: test.latestVersion, Channel: "stable"}, nil
				},
			}

			result, err := action.Run(context.Background(), action.Input{
				Operation: action.OperationAuto, EventName: "schedule", RefType: "branch", RefName: "main",
			}, deps)
			if err != nil {
				t.Fatal(err)
			}
			if checks != test.wantChecks || result.OfficialReviewPending != test.wantPaused {
				t.Fatalf("checks=%d result=%#v", checks, result)
			}
			if test.wantPaused && (result.OfficialReviewVersion != test.reviewVersion || result.Changed || result.Version != "1.3.0" || result.Tag != "v1.3.0" || result.Operation != "check") {
				t.Fatalf("paused result=%#v", result)
			}
		})
	}
}

func TestRunAutoPublishFailsClosedWhenReviewOrCandidateVersionIsNotSemVer(t *testing.T) {
	for _, test := range []struct {
		name             string
		reviewVersion    string
		candidateVersion string
	}{
		{name: "review", reviewVersion: "latest", candidateVersion: "1.4.0"},
		{name: "candidate", reviewVersion: "1.4.0", candidateVersion: "latest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := gitConfig()
			cfg.Update.Strategy = config.StrategyPublish
			cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
			cfg.Stores.Official.Enabled = true
			deps := action.Dependencies{
				Host:       platform.Host{OS: "linux", Arch: "amd64"},
				ResultDir:  filepath.Join(root, "results"),
				LoadConfig: func(string) (config.Config, error) { return cfg, nil },
				Inspect: func(context.Context, config.Project) (project.Info, error) {
					return project.Info{Root: root, PackageID: "cloud.lazycat.invalid-version", Version: "1.3.0"}, nil
				},
				SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
				Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
					return actionbuild.Result{}, nil
				},
				WaitingReviewVersion: func(context.Context, string) (string, bool, error) {
					return test.reviewVersion, true, nil
				},
				CheckImages: func(_ context.Context, request imageflow.Request) (imageflow.Result, error) {
					return imageflow.Result{}, fmt.Errorf("compare official review version %q with selected candidate version %q: invalid SemVer", request.OfficialReviewVersion, test.candidateVersion)
				},
			}
			_, err := action.Run(t.Context(), action.Input{Operation: action.OperationAuto, EventName: "schedule"}, deps)
			if err == nil || !strings.Contains(err.Error(), "CONFIG_INVALID") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestRunAutoPublishFailsClosedWhenPendingReviewVersionIsEmpty(t *testing.T) {
	root := t.TempDir()
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Stores.Official.Enabled = true
	cfg.Images = []config.Image{{
		ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver",
		Delivery: config.Delivery{Mode: "lazycat"},
	}}
	checkCalls := 0
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.empty-review", Version: "1.3.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		WaitingReviewVersion: func(context.Context, string) (string, bool, error) { return "", true, nil },
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			checkCalls++
			return imageflow.Result{Version: "1.4.0"}, nil
		},
	}
	_, err := action.Run(t.Context(), action.Input{Operation: action.OperationAuto, EventName: "schedule"}, deps)
	if err == nil || !strings.Contains(err.Error(), "CONFIG_INVALID") || checkCalls != 0 {
		t.Fatalf("err=%v checkCalls=%d", err, checkCalls)
	}
}

func TestRunAutoPublishCanDisableNewerCandidateContinuation(t *testing.T) {
	root := t.TempDir()
	disabled := false
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Stores.Official.Enabled = true
	cfg.Stores.Official.ContinueIfNewerVersion = &disabled
	checkCalls := 0
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.disabled-continuation", Version: "1.3.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		WaitingReviewVersion: func(context.Context, string) (string, bool, error) { return "1.2.0", true, nil },
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			checkCalls++
			return imageflow.Result{Version: "1.4.0"}, nil
		},
	}
	result, err := action.Run(t.Context(), action.Input{Operation: action.OperationAuto, EventName: "schedule"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OfficialReviewPending || result.OfficialReviewVersion != "1.2.0" || checkCalls != 0 {
		t.Fatalf("result=%#v checkCalls=%d", result, checkCalls)
	}
}

func TestRunCheckUpdatesVersionBuildsAndReturnsImageResults(t *testing.T) {
	root := t.TempDir()
	packageFile := filepath.Join(root, "package.yml")
	inspectCount := 0
	var built actionbuild.Request
	deps := action.Dependencies{
		Host:      platform.Host{OS: "linux", Arch: "arm64"},
		ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) {
			cfg := gitConfig()
			cfg.Update.Strategy = config.StrategyPull
			cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
			cfg.Images = []config.Image{{ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "direct"}}}
			return cfg, nil
		},
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCount++
			version := "1.0.0"
			if inspectCount > 1 {
				version = "2.0.0"
			}
			return project.Info{Root: root, PackageFile: packageFile, ManifestFile: filepath.Join(root, "manifest.yml"), Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			built = request
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			return imageflow.Result{Changed: true, Version: "2.0.0", Channel: "stable", Images: []imageflow.ImageResult{{ID: "web", Target: "service", Service: "web", Platform: "linux/amd64", SourceRef: "ghcr.io/acme/web:v2.0.0", SourceDigest: strings.Repeat("a", 64), DeliveryMode: "direct", DeliveredRef: "ghcr.io/acme/web:v2.0.0"}}}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck, EventName: "workflow_dispatch", RefType: "branch", RefName: "main"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != "check" || !result.Changed || result.Version != "2.0.0" || result.Tag != "v2.0.0" || result.UpdateStrategy != "pull" || result.Channel != "stable" {
		t.Fatalf("result=%#v", result)
	}
	if built.Version != "2.0.0" || built.Channel != "stable" || !strings.Contains(string(result.ImageResults), `"id":"web"`) {
		t.Fatalf("built=%#v images=%s", built, result.ImageResults)
	}
}

func TestRunCheckRejectsTagAndReleaseEventsBeforeImageCheck(t *testing.T) {
	tests := []action.Input{
		{Operation: action.OperationCheck, EventName: "push", RefType: "tag", RefName: "client-v0.1.38", Version: "0.1.38", Tag: "client-v0.1.38"},
		{Operation: action.OperationCheck, EventName: "release", Version: "0.1.38", Tag: "v0.1.38"},
		{Operation: action.OperationCheck, EventName: "workflow_dispatch", RefType: "tag", RefName: "v0.1.38", Version: "0.1.38", Tag: "v0.1.38"},
	}
	checkCalls := 0
	deps := action.Dependencies{
		Host: platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) {
			cfg := gitConfig()
			cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
			return cfg, nil
		},
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{PackageID: "cloud.lazycat.example", Version: "0.1.38"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) {
			t.Fatal("tag and release checks should fail before changing the project")
			return yamledit.Change{}, nil
		},
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			t.Fatal("tag and release checks should fail before building")
			return actionbuild.Result{}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			checkCalls++
			return imageflow.Result{Version: "0.1.38", Channel: "stable"}, nil
		},
	}
	for _, input := range tests {
		_, err := action.Run(context.Background(), input, deps)
		if err == nil || !strings.Contains(err.Error(), "not supported for tag or release events") {
			t.Fatalf("input=%#v err=%v", input, err)
		}
	}
	if checkCalls != 0 {
		t.Fatalf("check calls=%d", checkCalls)
	}
}

func TestRunCheckRejectsDirectPublishForNonVersionSourceImage(t *testing.T) {
	checkCalled := false
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Images = []config.Image{
		{ID: "db", Target: "service", Service: "db", Source: "postgres", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "lazycat"}},
		{ID: "web", Target: "service", Service: "web", Source: "example/web", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "lazycat"}},
	}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			checkCalled = true
			return imageflow.Result{}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck, ImageID: "db"}, deps)
	if err == nil || !strings.Contains(err.Error(), "must select version-source image") {
		t.Fatalf("err=%v", err)
	}
	if checkCalled {
		t.Fatal("image check ran for invalid direct-publish selection")
	}
}

func TestRunMapsImageVersionDowngrade(t *testing.T) {
	cfg := gitConfig()
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Images = []config.Image{{ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "direct"}}}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{PackageID: "cloud.lazycat.example", Version: "19.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			return imageflow.Result{}, imageflow.ErrVersionDowngrade
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck}, deps)
	var actionErr *action.Error
	if !errors.As(err, &actionErr) || actionErr.Code != action.CodeVersionDowngradeBlocked {
		t.Fatalf("err=%#v", err)
	}
}

func TestRunPreservesStructuredImageDeliveryError(t *testing.T) {
	cfg := gitConfig()
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Images = []config.Image{{ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "lazycat"}}}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			return imageflow.Result{}, fmt.Errorf("%w: %w", imageflow.ErrDeliveryFailed, &lpkgo.Error{
				Code: lpkgo.CodeRemoteUnavailable,
				Op:   "appstore.copy_image",
			})
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck}, deps)
	if err == nil {
		t.Fatal("expected image delivery failure")
	}
	message := err.Error()
	for _, expected := range []string{"IMAGE_COPY_FAILED", "upstream=REMOTE_UNAVAILABLE", "op=appstore.copy_image"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message=%q missing %q", message, expected)
		}
	}
}

func TestRunClassifiesMirrorVerificationFailureWithoutCallingItACopy(t *testing.T) {
	cfg := gitConfig()
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Images = []config.Image{{
		ID: "web", Target: "service", Service: "web", Source: "docker.io/acme/web", Channel: "stable", Sort: "semver",
		Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "docker.1ms.run/acme/web:{tag}", RequireDigestMatch: true},
	}}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			return imageflow.Result{}, fmt.Errorf("%w: %w: registry timeout", imageflow.ErrMirrorVerificationFailed, delivery.ErrMirrorVerification)
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck}, deps)
	if err == nil {
		t.Fatal("expected mirror verification failure")
	}
	message := err.Error()
	if !strings.Contains(message, "MIRROR_VERIFICATION_FAILED") || strings.Contains(message, "COPY") || strings.Contains(strings.ToLower(message), "delivery") {
		t.Fatalf("message=%q", message)
	}
}

func TestRunAutoManualGitBuildUsesCurrentPackageVersionWhenInputIsEmpty(t *testing.T) {
	root := t.TempDir()
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: "1.2.3"}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			if version != "1.2.3" {
				t.Fatalf("version=%q", version)
			}
			return yamledit.Change{Old: version, New: version}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != "build" || result.Version != "1.2.3" || result.Tag != "v1.2.3" || result.UpdateStrategy != "publish" {
		t.Fatalf("result=%#v", result)
	}
}

func gitConfig() config.Config {
	enabled := true
	return config.Config{
		Version: 1,
		Project: config.Project{Root: ".", BuildConfig: "lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk"},
		Update:  config.Update{Strategy: config.StrategyPull, VersionSource: config.VersionSource{Type: config.VersionSourceGit}},
		Build:   config.Build{RunBuildScript: &enabled},
	}
}
