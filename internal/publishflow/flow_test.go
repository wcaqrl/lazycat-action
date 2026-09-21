package publishflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/auth"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/publishflow"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	"github.com/wcaqrl/lazycat-action/internal/storelookup"
)

const artifactSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestPublishOfficialVerifiesAndPublishes(t *testing.T) {
	published := false
	flow := officialFlow()
	flow.PublishOfficial = func(_ context.Context, request official.Request) (official.Result, error) {
		published = true
		if request.PackageID != "cloud.lazycat.example" || request.Version != "1.2.3" || request.Changelog != "Release notes" {
			t.Fatalf("request=%#v", request)
		}
		return official.Result{Published: true, PackageID: request.PackageID, Version: request.Version, SHA256: request.SHA256}, nil
	}
	result, err := flow.Publish(t.Context(), publishflow.Request{
		Config: officialConfig(), Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes", ExpectedSHA256: artifactSHA,
	})
	if err != nil || !published || result.Official == nil || !result.Official.Published {
		t.Fatalf("published=%t result=%#v err=%v", published, result, err)
	}
}

func TestPublishOfficialSkipsExistingVersion(t *testing.T) {
	flow := officialFlow()
	flow.LookupVersion = func(context.Context, storelookup.Request) (storelookup.Result, error) {
		return storelookup.Result{OnlineVersion: "1.2.3"}, nil
	}
	flow.PublishOfficial = func(context.Context, official.Request) (official.Result, error) {
		t.Fatal("publisher must not run for an existing version")
		return official.Result{}, nil
	}
	cfg := officialConfig()
	cfg.Stores.Official.SkipIfVersionExists = true
	result, err := flow.Publish(t.Context(), publishflow.Request{
		Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk", Version: "1.2.3", Changelog: "Release notes",
	})
	if err != nil || result.Official == nil || !result.Official.Skipped || result.Official.SkipReason != "version-already-online" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestPublishOfficialRejectsIntegrityMismatch(t *testing.T) {
	flow := officialFlow()
	_, err := flow.Publish(t.Context(), publishflow.Request{
		Config: officialConfig(), Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes", ExpectedSHA256: strings.Repeat("b", 64),
	})
	if !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestPublishOfficialRequiresPublishStrategyAndEnabledStore(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*config.Config)
		want   error
	}{
		{name: "strategy", mutate: func(cfg *config.Config) { cfg.Update.Strategy = config.StrategyPull }, want: publishflow.ErrPublishStrategyRequired},
		{name: "disabled", mutate: func(cfg *config.Config) { cfg.Stores.Official.Enabled = false }, want: publishflow.ErrStoreDisabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := officialConfig()
			test.mutate(&cfg)
			_, err := officialFlow().Publish(t.Context(), publishflow.Request{Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk", Version: "1.2.3", Changelog: "Release notes"})
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v want=%v", err, test.want)
			}
		})
	}
}

func officialFlow() publishflow.Flow {
	return publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) {
			return lpkcheck.Result{Path: "/repo/dist/app.lpk", PackageID: "cloud.lazycat.example", Version: "1.2.3", SHA256: artifactSHA, TargetPlatform: "linux/amd64"}, nil
		},
		PrecheckOfficial: func(context.Context, string) error { return nil },
		ResolveAuth: func(context.Context) (platformauth.Result, error) {
			return platformauth.Result{Provider: auth.StaticToken("test-token")}, nil
		},
	}
}

func officialConfig() config.Config {
	return config.Config{
		Version: 1,
		Project: config.Project{TargetArch: "amd64"},
		Update:  config.Update{Strategy: config.StrategyPublish, VersionSource: config.VersionSource{Type: config.VersionSourceGit}},
		Stores:  config.Stores{Official: config.OfficialStore{Enabled: true}},
	}
}

func projectInfo() project.Info {
	return project.Info{Root: "/repo", PackageID: "cloud.lazycat.example", Version: "1.2.3", Name: "Example"}
}
