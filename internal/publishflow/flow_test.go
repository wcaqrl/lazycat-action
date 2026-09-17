package publishflow_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/publishflow"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	private "github.com/wcaqrl/lazycat-action/internal/store/private"
	"github.com/wcaqrl/lazycat-action/internal/storelookup"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

const artifactSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestPublishOfficialPropagatesVerifiedArtifactAndRetryPolicy(t *testing.T) {
	var logs bytes.Buffer
	var published official.Request
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	flow := publishflow.Flow{
		Logger: logger,
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) {
			return verifiedArtifact(), nil
		},
		PrecheckOfficial: passOfficialPrecheck,
		ResolveAuth: func(context.Context) (platformauth.Result, error) {
			return platformauth.Result{Provider: auth.StaticToken("secret")}, nil
		},
		PublishOfficial: func(_ context.Context, request official.Request) (official.Result, error) {
			published = request
			return official.Result{Published: true, PackageID: request.PackageID, Version: request.Version, SHA256: request.SHA256, UploadURL: "/upload.lpk"}, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Official.Enabled = true
	cfg.Stores.Official.Retry = config.OfficialRetry{
		Enabled: true, MaxAttempts: 4, InitialDelay: 3 * time.Second, MaxDelay: 45 * time.Second,
	}
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Official == nil || !result.Official.Published || result.Private != nil || published.ProjectRoot != "/repo" || published.PackageID != "cloud.lazycat.example" || published.SHA256 != artifactSHA || published.Changelog != "Release notes" || strings.Join(published.Locales, ",") != "zh,en" {
		t.Fatalf("result=%#v request=%#v", result, published)
	}
	if published.Retry != cfg.Stores.Official.Retry || published.Logger != logger {
		t.Fatalf("retry=%#v logger=%p want retry=%#v logger=%p", published.Retry, published.Logger, cfg.Stores.Official.Retry, logger)
	}
	for _, expected := range []string{"store publication started", "LPK publication artifact verified", "store publication completed", "store=official"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("logs missing %q: %s", expected, logs.String())
		}
	}
}

func TestPublishOfficialPrecheckRunsAfterLookupBeforeAuthenticationAndPublish(t *testing.T) {
	var calls []string
	precheckErr := &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "store.official.precheck"}
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) {
			calls = append(calls, "verify")
			return verifiedArtifact(), nil
		},
		PrecheckOfficial: func(_ context.Context, path string) error {
			calls = append(calls, "precheck")
			if path != verifiedArtifact().Path {
				t.Fatalf("precheck path=%q", path)
			}
			return precheckErr
		},
		LookupVersion: func(context.Context, storelookup.Request) (storelookup.Result, error) {
			calls = append(calls, "lookup")
			return storelookup.Result{}, lpkgo.ErrNotFound
		},
		ResolveAuth: func(context.Context) (platformauth.Result, error) {
			t.Fatal("official precheck failure must prevent authentication")
			return platformauth.Result{}, nil
		},
		PublishOfficial: func(context.Context, official.Request) (official.Result, error) {
			t.Fatal("official precheck failure must prevent publishing")
			return official.Result{}, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Official.Enabled = true
	cfg.Stores.Official.SkipIfVersionExists = true
	_, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes",
	})
	if !errors.Is(err, precheckErr) {
		t.Fatalf("err=%v", err)
	}
	if strings.Join(calls, ",") != "verify,lookup,precheck" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestFlowPublishesPrivateStoreWithEnvironmentAndMetadataDefaults(t *testing.T) {
	var options private.Options
	var published private.Request
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"APPSTORE_URL": "https://store.example.com", "APPSTORE_TOKEN": "lcst_secret", "APP_ID": "42"}
			value, found := values[name]
			return value, found
		},
		NewPrivate: func(input private.Options) (publishflow.PrivatePublisher, error) {
			options = input
			return privatePublisherFunc(func(_ context.Context, request private.Request) (private.Result, error) {
				published = request
				return private.Result{Published: true, AppID: request.AppID, PackageID: request.PackageID, Version: request.Version, DownloadURL: request.DownloadURL, SHA256: request.SHA256}, nil
			}), nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Private == nil || result.Official != nil || options.BaseURL != "https://store.example.com" || options.Token != "lcst_secret" || published.AppID != "42" || published.Name != "Example" || published.Summary != "Example summary" || published.SHA256 != artifactSHA {
		t.Fatalf("result=%#v options=%#v request=%#v", result, options, published)
	}
}

func TestFlowRetriesTransientPrivateLookupBeforePublishing(t *testing.T) {
	lookupAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/packages/cloud.lazycat.example/latest-version" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		lookupAttempts++
		if lookupAttempts == 1 {
			http.Error(response, "transient upstream failure", http.StatusBadGateway)
			return
		}
		_, _ = response.Write([]byte(`{"packageId":"cloud.lazycat.example","latestVersion":{"version":"1.2.2","createdAt":"2026-07-11T00:00:00Z"}}`))
	}))
	defer server.Close()

	published := false
	flow := publishflow.Flow{
		Verify:        func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupVersion: storelookup.Default,
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"APPSTORE_URL": server.URL, "APPSTORE_TOKEN": "lcst_secret"}
			value, found := values[name]
			return value, found
		},
		NewPrivate: func(private.Options) (publishflow.PrivatePublisher, error) {
			return privatePublisherFunc(func(_ context.Context, request private.Request) (private.Result, error) {
				published = true
				return private.Result{Published: true, PackageID: request.PackageID, Version: request.Version, DownloadURL: request.DownloadURL, SHA256: request.SHA256}, nil
			}), nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	cfg.Stores.Private.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lookupAttempts != 2 || !published || result.Private == nil || !result.Private.Published || result.Private.OnlineVersion != "1.2.2" {
		t.Fatalf("lookupAttempts=%d published=%v result=%#v", lookupAttempts, published, result)
	}
}

func TestFlowVerifiesConfiguredARM64Artifact(t *testing.T) {
	var verifyRequest lpkcheck.Request
	flow := publishflow.Flow{Verify: func(_ context.Context, request lpkcheck.Request) (lpkcheck.Result, error) {
		verifyRequest = request
		artifact := verifiedArtifact()
		artifact.TargetPlatform = "linux/arm64"
		return artifact, nil
	}}
	cfg := publishConfig()
	cfg.Project.TargetArch = "arm64"
	cfg.Stores.Private.Enabled = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk",
		ExpectedSHA256: artifactSHA, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verifyRequest.Target.Platform() != "linux/arm64" || result.Artifact.TargetPlatform != "linux/arm64" {
		t.Fatalf("verify=%#v result=%#v", verifyRequest, result)
	}
}

func TestFlowSkipsOfficialPublishWhenOnlineVersionMatches(t *testing.T) {
	t.Setenv("LZC_APPSTORE_COS_DOMAIN", "cos.example.com")
	lookupCalls := 0
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		PrecheckOfficial: func(context.Context, string) error {
			t.Fatal("official precheck must not run for an equal online version")
			return nil
		},
		LookupVersion: func(_ context.Context, request storelookup.Request) (storelookup.Result, error) {
			lookupCalls++
			if request.Store != storelookup.StoreOfficial || request.PackageID != "cloud.lazycat.example" || request.BaseURL != "https://cos.example.com/appstore/metarepo" {
				t.Fatalf("lookup request=%#v", request)
			}
			return storelookup.Result{OnlineVersion: "1.2.3"}, nil
		},
		ResolveAuth: func(context.Context) (platformauth.Result, error) {
			t.Fatal("official authentication must not run for an equal online version")
			return platformauth.Result{}, nil
		},
		PublishOfficial: func(context.Context, official.Request) (official.Result, error) {
			t.Fatal("official publish must not run for an equal online version")
			return official.Result{}, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Official.Enabled = true
	cfg.Stores.Official.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if lookupCalls != 1 || result.Official == nil || result.Official.Published || !result.Official.Skipped || result.Official.OnlineVersion != "1.2.3" || result.Official.PackageID != "cloud.lazycat.example" || result.Official.SHA256 != artifactSHA {
		t.Fatalf("lookupCalls=%d result=%#v", lookupCalls, result)
	}
	if result.Official.SkipReason != "version-already-online" {
		t.Fatalf("skip reason=%q", result.Official.SkipReason)
	}
}

func TestFlowSkipsOfficialPublishWhenOnlineVersionIsNewer(t *testing.T) {
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) {
			artifact := verifiedArtifact()
			artifact.Version = "7.7.406"
			return artifact, nil
		},
		PrecheckOfficial: passOfficialPrecheck,
		LookupVersion: func(_ context.Context, request storelookup.Request) (storelookup.Result, error) {
			if request.Store != storelookup.StoreOfficial || request.PackageID != "cloud.lazycat.example" {
				t.Fatalf("lookup request=%#v", request)
			}
			return storelookup.Result{OnlineVersion: "7.8.138"}, nil
		},
		ResolveAuth: func(context.Context) (platformauth.Result, error) {
			t.Fatal("official authentication must not run for a newer online version")
			return platformauth.Result{}, nil
		},
		PublishOfficial: func(context.Context, official.Request) (official.Result, error) {
			t.Fatal("official publish must not run for a newer online version")
			return official.Result{}, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Official.Enabled = true
	cfg.Stores.Official.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "7.7.406", Changelog: "Release notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Official == nil || !result.Official.Skipped || result.Official.OnlineVersion != "7.8.138" || result.Official.SkipReason != "online-version-newer" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowSkipsPrivatePublishWithSecretGroupCodes(t *testing.T) {
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{
				"APPSTORE_URL":              "https://store.example.com",
				"PRIVATE_STORE_GROUP_CODES": " ABC123, LATE23 ,,",
			}
			value, found := values[name]
			return value, found
		},
		LookupVersion: func(_ context.Context, request storelookup.Request) (storelookup.Result, error) {
			if request.Store != storelookup.StorePrivate || request.BaseURL != "https://store.example.com" || strings.Join(request.GroupCodes, ",") != "ABC123,LATE23" {
				t.Fatalf("lookup request=%#v", request)
			}
			return storelookup.Result{OnlineVersion: "1.2.3"}, nil
		},
		NewPrivate: func(private.Options) (publishflow.PrivatePublisher, error) {
			t.Fatal("private publisher must not be configured for an equal online version")
			return nil, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	cfg.Stores.Private.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Private == nil || result.Private.Published || !result.Private.Skipped || result.Private.OnlineVersion != "1.2.3" || result.Private.PackageID != "cloud.lazycat.example" || result.Private.SHA256 != artifactSHA {
		t.Fatalf("result=%#v", result)
	}
	if result.Private.SkipReason != "version-already-online" {
		t.Fatalf("skip reason=%q", result.Private.SkipReason)
	}
}

func TestFlowSkipsPrivatePublishWhenOnlineVersionIsNewer(t *testing.T) {
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) {
			artifact := verifiedArtifact()
			artifact.Version = "7.7.406"
			return artifact, nil
		},
		LookupVersion: func(context.Context, storelookup.Request) (storelookup.Result, error) {
			return storelookup.Result{OnlineVersion: "7.8.138"}, nil
		},
		NewPrivate: func(private.Options) (publishflow.PrivatePublisher, error) {
			t.Fatal("private publisher must not be configured for a newer online version")
			return nil, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	cfg.Stores.Private.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "7.7.406", DownloadURL: "https://github.com/acme/example/releases/download/v7.7.406/app.lpk", ExpectedSHA256: artifactSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Private == nil || !result.Private.Skipped || result.Private.OnlineVersion != "7.8.138" || result.Private.SkipReason != "online-version-newer" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowStoreLookupOutcomes(t *testing.T) {
	tests := []struct {
		name             string
		lookupVersion    string
		lookupErr        error
		wantErr          bool
		wantPublished    bool
		wantOnline       string
		wantSkipped      bool
		wantReason       string
		allowDowngrade   bool
		candidateVersion string
	}{
		{name: "different version publishes", lookupVersion: "1.2.2", wantPublished: true, wantOnline: "1.2.2"},
		{name: "newer online version skips", lookupVersion: "1.2.4", wantOnline: "1.2.4", wantSkipped: true, wantReason: "online-version-newer"},
		{name: "explicit downgrade publishes", lookupVersion: "1.2.4", wantPublished: true, wantOnline: "1.2.4", allowDowngrade: true},
		{name: "non semver online version publishes", lookupVersion: "latest", wantPublished: true, wantOnline: "latest"},
		{name: "non semver candidate version publishes", lookupVersion: "2.0.0", wantPublished: true, wantOnline: "2.0.0", candidateVersion: "latest"},
		{name: "not found publishes", lookupErr: lpkgo.ErrNotFound, wantPublished: true},
		{name: "lookup failure stops", lookupErr: &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			published := false
			candidateVersion := test.candidateVersion
			if candidateVersion == "" {
				candidateVersion = "1.2.3"
			}
			flow := publishflow.Flow{
				Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) {
					artifact := verifiedArtifact()
					artifact.Version = candidateVersion
					return artifact, nil
				},
				PrecheckOfficial: passOfficialPrecheck,
				LookupVersion: func(context.Context, storelookup.Request) (storelookup.Result, error) {
					return storelookup.Result{OnlineVersion: test.lookupVersion}, test.lookupErr
				},
				ResolveAuth: func(context.Context) (platformauth.Result, error) {
					return platformauth.Result{Provider: auth.StaticToken("secret")}, nil
				},
				PublishOfficial: func(_ context.Context, request official.Request) (official.Result, error) {
					published = true
					return official.Result{Published: true, PackageID: request.PackageID, Version: request.Version, SHA256: request.SHA256}, nil
				},
			}
			cfg := publishConfig()
			cfg.Stores.Official.Enabled = true
			cfg.Stores.Official.SkipIfVersionExists = true
			cfg.Update.AllowDowngrade = test.allowDowngrade
			result, err := flow.Publish(context.Background(), publishflow.Request{
				Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk", Version: candidateVersion, Changelog: "Release notes",
			})
			if (err != nil) != test.wantErr || published != test.wantPublished {
				t.Fatalf("published=%v result=%#v err=%v", published, result, err)
			}
			if !test.wantErr && result.Official.OnlineVersion != test.wantOnline {
				t.Fatalf("result=%#v", result)
			}
			if !test.wantErr && (result.Official.Skipped != test.wantSkipped || result.Official.SkipReason != test.wantReason) {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestFlowRejectsUnsafePublishingStatesAndDryRunSkipsRemoteCalls(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config, *publishflow.Request)
		wantErr error
	}{
		{name: "pull strategy", mutate: func(cfg *config.Config, _ *publishflow.Request) { cfg.Update.Strategy = config.StrategyPull }, wantErr: publishflow.ErrPublishStrategyRequired},
		{name: "disabled store", mutate: func(cfg *config.Config, _ *publishflow.Request) { cfg.Stores.Private.Enabled = false }, wantErr: publishflow.ErrStoreDisabled},
		{name: "missing private URL", mutate: func(_ *config.Config, request *publishflow.Request) { request.DownloadURL = "" }, wantErr: publishflow.ErrReleaseAssetMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := publishConfig()
			cfg.Stores.Private.Enabled = true
			request := publishflow.Request{Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk", Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA}
			test.mutate(&request.Config, &request)
			_, err := (publishflow.Flow{}).Publish(context.Background(), request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("err=%v", err)
			}
		})
	}

	remoteCalled := false
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupVersion: func(context.Context, storelookup.Request) (storelookup.Result, error) {
			t.Fatal("dry-run must not query stores")
			return storelookup.Result{}, nil
		},
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"APPSTORE_URL": "https://store.example.com", "APPSTORE_TOKEN": "lcst_secret"}
			value, found := values[name]
			return value, found
		},
		NewPrivate: func(private.Options) (publishflow.PrivatePublisher, error) {
			remoteCalled = true
			return nil, errors.New("must not create private client")
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	cfg.Stores.Private.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA, DryRun: true,
	})
	if err != nil || remoteCalled || result.Private == nil || result.Private.Published {
		t.Fatalf("result=%#v remoteCalled=%v err=%v", result, remoteCalled, err)
	}
}

func TestFlowRejectsReleaseAssetSHA256Mismatch(t *testing.T) {
	flow := publishflow.Flow{Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil }}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	_, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk",
		ExpectedSHA256: strings.Repeat("b", 64),
	})
	if !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
		t.Fatalf("err=%v", err)
	}
}

type privatePublisherFunc func(context.Context, private.Request) (private.Result, error)

func (function privatePublisherFunc) Publish(ctx context.Context, request private.Request) (private.Result, error) {
	return function(ctx, request)
}

func verifiedArtifact() lpkcheck.Result {
	return lpkcheck.Result{Path: "/repo/dist/app.lpk", PackageID: "cloud.lazycat.example", Version: "1.2.3", SHA256: artifactSHA, Size: 123, TargetPlatform: "linux/amd64"}
}

func passOfficialPrecheck(context.Context, string) error { return nil }

func projectInfo() project.Info {
	return project.Info{Root: "/repo", PackageID: "cloud.lazycat.example", Version: "1.2.3", Name: "Example", Description: "Example summary", Output: "/repo/dist/app.lpk"}
}

func publishConfig() config.Config {
	return config.Config{
		Update: config.Update{Strategy: config.StrategyPublish},
		Stores: config.Stores{Official: config.OfficialStore{Locales: []string{"zh", "en"}}},
	}
}
