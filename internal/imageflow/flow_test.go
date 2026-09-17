package imageflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/delivery"
	"github.com/wcaqrl/lazycat-action/internal/imageflow"
	"github.com/wcaqrl/lazycat-action/internal/imagemirror"
	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/registry"
	"github.com/wcaqrl/lazycat-action/internal/versioning"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

func TestFlowChecksAllImagesAndUpdatesOnlyChangedTarget(t *testing.T) {
	var logs bytes.Buffer
	registryClient := &fakeRegistry{bySource: map[string][]versioning.Candidate{
		"docker.io/library/postgres": {{Tag: "17.1.0", Digest: digest("d"), Created: created(1)}},
		"ghcr.io/acme/web":           {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}},
	}}
	deliverer := &fakeDeliverer{}
	var applied []manifestedit.Update
	flow := imageflow.Flow{
		Registry:  registryClient,
		Deliverer: deliverer,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{
				{ID: "db", RuntimeRef: "docker.io/library/postgres:17.1.0", UpstreamRef: "docker.io/library/postgres:17.1.0"},
				{ID: "web", RuntimeRef: "ghcr.io/acme/web:v1.0.0", UpstreamRef: "ghcr.io/acme/web:v1.0.0"},
			}, nil
		},
		ApplyManifest: func(_ string, updates []manifestedit.Update) ([]manifestedit.Change, error) {
			applied = append(applied, updates...)
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "2.0.0" || result.Channel != "stable" || len(result.Images) != 2 {
		t.Fatalf("result=%#v", result)
	}
	if len(applied) != 1 || applied[0].Target.ID != "web" {
		t.Fatalf("applied=%#v", applied)
	}
	if registryClient.calls != 2 || deliverer.calls != 2 {
		t.Fatalf("registry calls=%d delivery calls=%d", registryClient.calls, deliverer.calls)
	}
	for _, expected := range []string{"Docker image update started", "querying Docker image versions", "Docker image version selected", "Docker image delivery completed", "Docker image update completed"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("logs missing %q: %s", expected, logs.String())
		}
	}
}

func TestFlowRecoversMirrorSourceBeforeRegistryQuery(t *testing.T) {
	mirrors, err := imagemirror.FromEnvironment(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	resolver := delivery.Resolver{Mirrors: mirrors}
	registryClient := &fakeRegistry{bySource: map[string][]versioning.Candidate{
		"docker.io/calciumion/new-api": {{Tag: "v1.2.3", Digest: digest("a"), Created: created(1)}},
	}}
	deliverer := &fakeDeliverer{}
	cfg := config.Config{
		Update: config.Update{VersionSource: config.VersionSource{Type: config.VersionSourceImage, Image: "web"}},
		Images: []config.Image{{
			ID: "web", Target: "service", Service: "web", Channel: "stable", Sort: "semver", VersionTemplate: "{version}", Delivery: config.Delivery{Mode: "mirror"},
		}},
	}
	flow := imageflow.Flow{
		Registry: registryClient, Deliverer: deliverer, ResolveImage: resolver.ResolveImage,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "docker.1ms.run/calciumion/new-api:v1.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if registryClient.calls != 1 || deliverer.last.Image.Source != "docker.io/calciumion/new-api" || deliverer.last.Image.Delivery.ImageTemplate != "docker.1ms.run/calciumion/new-api:{tag}" {
		t.Fatalf("registry calls=%d delivery=%#v", registryClient.calls, deliverer.last)
	}
	if result.Images[0].SourceRef != "docker.io/calciumion/new-api:v1.2.3" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowRejectsUnrecoverableMirrorSourceBeforeRegistryQuery(t *testing.T) {
	mirrors, err := imagemirror.FromEnvironment(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	resolver := delivery.Resolver{Mirrors: mirrors}
	registryClient := &fakeRegistry{}
	deliverer := &fakeDeliverer{}
	flow := imageflow.Flow{
		Registry: registryClient, Deliverer: deliverer, ResolveImage: resolver.ResolveImage,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "unknown.example/acme/web:v1"}}, nil
		},
	}
	cfg := config.Config{Images: []config.Image{{
		ID: "web", Target: "service", Service: "web", Channel: "stable", Sort: "semver", VersionTemplate: "{version}", Delivery: config.Delivery{Mode: "mirror"},
	}}}
	_, err = flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err == nil || !strings.Contains(err.Error(), "source cannot be recovered") {
		t.Fatalf("err=%v", err)
	}
	if registryClient.calls != 0 || deliverer.calls != 0 {
		t.Fatalf("registry calls=%d delivery calls=%d", registryClient.calls, deliverer.calls)
	}
}

func TestFlowExplicitNonDriverImageKeepsPackageVersion(t *testing.T) {
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"docker.io/library/postgres": {{Tag: "18.0.0", Digest: digest("d"), Created: created(1)}}}},
		Deliverer: &fakeDeliverer{},
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "db", RuntimeRef: "docker.io/library/postgres:17.1.0", UpstreamRef: "docker.io/library/postgres:17.1.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "db", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}, ImageID: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "1.0.0" || result.Channel != "" || len(result.Images) != 1 || result.Images[0].ID != "db" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowUsesConfiguredARM64Target(t *testing.T) {
	registryClient := &fakeRegistry{bySource: map[string][]versioning.Candidate{
		"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}},
	}}
	deliverer := &fakeDeliverer{}
	cfg := imageConfig()
	cfg.Project.TargetArch = "arm64"
	cfg.Images = cfg.Images[1:]
	flow := imageflow.Flow{
		Registry: registryClient, Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "ghcr.io/acme/web:v1.0.0", UpstreamRef: "ghcr.io/acme/web:v1.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := platform.Target{OS: "linux", Arch: "arm64"}
	if registryClient.target != wantTarget || deliverer.last.Target != wantTarget || result.Images[0].Platform != "linux/arm64" {
		t.Fatalf("registry=%#v delivery=%#v result=%#v", registryClient.target, deliverer.last.Target, result)
	}
}

func TestFlowBlocksVersionSourceDowngradeBeforeDelivery(t *testing.T) {
	deliverer := &fakeDeliverer{}
	applied := 0
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Channel = "custom"
	cfg.Images[0].Sort = "created"
	cfg.Images[0].TagRegex = `^\d+\.\d+$`
	cfg.Images[0].VersionRegex = `^(?P<version>\d+\.\d+)$`
	cfg.Images[0].VersionTemplate = "{version}.0"
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "18.0", Digest: digest("e"), Created: created(11)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:19", UpstreamRef: "ghcr.io/acme/web:19.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			applied++
			return nil, nil
		},
	}
	_, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "19.0.0"}})
	if !errors.Is(err, imageflow.ErrVersionDowngrade) {
		t.Fatalf("err=%v", err)
	}
	if deliverer.calls != 0 || applied != 0 {
		t.Fatalf("deliveries=%d applied=%d", deliverer.calls, applied)
	}
}

func TestFlowOfficialReviewGateUsesSelectedVersionSourceCandidate(t *testing.T) {
	for _, test := range []struct {
		name          string
		reviewVersion string
		wantPaused    bool
	}{
		{name: "equal review pauses", reviewVersion: "2.0.0", wantPaused: true},
		{name: "newer review pauses", reviewVersion: "2.1.0", wantPaused: true},
		{name: "older review continues", reviewVersion: "1.9.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			deliverer := &fakeDeliverer{}
			registryClient := &fakeRegistry{bySource: map[string][]versioning.Candidate{
				"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}},
			}}
			cfg := imageConfig()
			flow := imageflow.Flow{
				Registry: registryClient, Deliverer: deliverer,
				ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
					return []manifestedit.Current{
						{ID: "db", RuntimeRef: "docker.io/library/postgres:17.1.0", UpstreamRef: "docker.io/library/postgres:17.1.0"},
						{ID: "web", RuntimeRef: "ghcr.io/acme/web:v1.0.0", UpstreamRef: "ghcr.io/acme/web:v1.0.0"},
					}, nil
				},
				ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
					return []manifestedit.Change{{ID: "web", Changed: true}}, nil
				},
			}
			result, err := flow.Check(t.Context(), imageflow.Request{
				Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"},
				ImageID: "web", OfficialReviewVersion: test.reviewVersion,
			})
			if test.wantPaused {
				if err == nil || !errors.Is(err, imageflow.ErrOfficialReviewCoversCandidate) || deliverer.calls != 0 {
					t.Fatalf("err=%v deliveries=%d", err, deliverer.calls)
				}
				return
			}
			if err != nil || result.Version != "2.0.0" || deliverer.calls != 1 {
				t.Fatalf("result=%#v err=%v deliveries=%d", result, err, deliverer.calls)
			}
		})
	}
}

func TestFlowOfficialReviewGateRejectsNonSemVer(t *testing.T) {
	for _, reviewVersion := range []string{"latest", "1.2"} {
		t.Run(reviewVersion, func(t *testing.T) {
			cfg := imageConfig()
			cfg.Images = cfg.Images[1:]
			flow := imageflow.Flow{
				Registry: &fakeRegistry{bySource: map[string][]versioning.Candidate{
					"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}},
				}},
				Deliverer: &fakeDeliverer{},
				ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
					return []manifestedit.Current{{ID: "web", RuntimeRef: "ghcr.io/acme/web:v1.0.0", UpstreamRef: "ghcr.io/acme/web:v1.0.0"}}, nil
				},
			}
			_, err := flow.Check(t.Context(), imageflow.Request{
				Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}, OfficialReviewVersion: reviewVersion,
			})
			if err == nil || errors.Is(err, imageflow.ErrOfficialReviewCoversCandidate) || !strings.Contains(err.Error(), "compare official review") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestFlowOfficialReviewGatePlansMutablePatchFromPersistedDigest(t *testing.T) {
	for _, test := range []struct {
		name          string
		reviewVersion string
		sourceDigest  string
		digestChanged bool
		wantPaused    bool
		wantVersion   string
	}{
		{name: "changed digest newer patch continues", reviewVersion: "1.4.6", sourceDigest: digest("b"), digestChanged: true, wantVersion: "1.4.7"},
		{name: "equal digest current review pauses", reviewVersion: "1.4.6", sourceDigest: digest("a"), wantPaused: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := mutableImageConfig()
			deliverer := &fakeDeliverer{digestChanged: test.digestChanged, currentDigest: digest("a")}
			flow := imageflow.Flow{
				Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "latest", Digest: test.sourceDigest, Created: created(12)}}}},
				Deliverer: deliverer,
				ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
					return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:current", UpstreamRef: "ghcr.io/acme/web:latest@" + digest("a")}}, nil
				},
				ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
					return []manifestedit.Change{{ID: "web", Changed: true}}, nil
				},
			}
			result, err := flow.Check(t.Context(), imageflow.Request{
				Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.4.6"}, OfficialReviewVersion: test.reviewVersion,
			})
			if test.wantPaused {
				if err == nil || !errors.Is(err, imageflow.ErrOfficialReviewCoversCandidate) || deliverer.calls != 0 {
					t.Fatalf("err=%v deliveries=%d", err, deliverer.calls)
				}
				return
			}
			if err != nil || result.Version != test.wantVersion || deliverer.calls != 1 {
				t.Fatalf("result=%#v err=%v deliveries=%d", result, err, deliverer.calls)
			}
		})
	}
}

func TestFlowOfficialReviewGateRejectsMutableDigestPlanDrift(t *testing.T) {
	cfg := mutableImageConfig()
	deliverer := &fakeDeliverer{digestChanged: false, currentDigest: digest("a")}
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "latest", Digest: digest("b"), Created: created(12)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:current", UpstreamRef: "ghcr.io/acme/web:latest@" + digest("a")}}, nil
		},
	}
	_, err := flow.Check(t.Context(), imageflow.Request{
		Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.4.6"}, OfficialReviewVersion: "1.4.6",
	})
	if err == nil || !strings.Contains(err.Error(), "digest plan changed") {
		t.Fatalf("err=%v", err)
	}
}

func TestFlowAllowsExplicitVersionSourceDowngrade(t *testing.T) {
	deliverer := &fakeDeliverer{}
	cfg := imageConfig()
	cfg.Update.AllowDowngrade = true
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Channel = "custom"
	cfg.Images[0].Sort = "created"
	cfg.Images[0].TagRegex = `^\d+\.\d+$`
	cfg.Images[0].VersionRegex = `^(?P<version>\d+\.\d+)$`
	cfg.Images[0].VersionTemplate = "{version}.0"
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "18.0", Digest: digest("e"), Created: created(11)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:19", UpstreamRef: "ghcr.io/acme/web:19.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "19.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "18.0.0" || !result.Changed || deliverer.calls != 1 {
		t.Fatalf("deliveries=%d result=%#v", deliverer.calls, result)
	}
}

func TestFlowAllowsEqualVersionImageRefresh(t *testing.T) {
	deliverer := &fakeDeliverer{}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "v19.0.0", Digest: digest("f"), Created: created(11)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:old", UpstreamRef: "ghcr.io/acme/web:19.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "19.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "19.0.0" || !result.Changed || deliverer.calls != 1 {
		t.Fatalf("deliveries=%d result=%#v", deliverer.calls, result)
	}
}

func TestFlowUsesUpdatedRegistryRankingAndRefreshesLazyCatImage(t *testing.T) {
	updated := time.Date(2026, 7, 12, 8, 30, 0, 0, time.UTC)
	registryClient := &fakeRegistry{bySource: map[string][]versioning.Candidate{
		"docker.io/zerodeng/sublink-pro": {{Tag: "v1.2.15", Digest: digest("f"), Updated: updated}},
	}}
	deliverer := &fakeDeliverer{}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Source = "docker.io/zerodeng/sublink-pro"
	cfg.Images[0].Sort = "updated"
	cfg.Images[0].Delivery.Mode = "lazycat"
	flow := imageflow.Flow{
		Registry:  registryClient,
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:v1.2.15", UpstreamRef: "docker.io/zerodeng/sublink-pro:v1.2.15"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return nil, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.2.15"}})
	if err != nil {
		t.Fatal(err)
	}
	if registryClient.lastFilter.UpdatedRule == nil || registryClient.lastFilter.SemVerRule != nil {
		t.Fatalf("filter=%#v", registryClient.lastFilter)
	}
	if deliverer.calls != 1 || result.Version != "1.2.15" {
		t.Fatalf("deliveries=%d result=%#v", deliverer.calls, result)
	}
}

func TestFlowUpdatedSelectionStillHonorsDowngradeGuard(t *testing.T) {
	deliverer := &fakeDeliverer{}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Sort = "updated"
	flow := imageflow.Flow{
		Registry: &fakeRegistry{bySource: map[string][]versioning.Candidate{
			"ghcr.io/acme/web": {{Tag: "v1.2.15", Digest: digest("e"), Updated: created(12)}},
		}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "ghcr.io/acme/web:v1.2.26", UpstreamRef: "ghcr.io/acme/web:v1.2.26"}}, nil
		},
	}
	_, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.2.26"}})
	if !errors.Is(err, imageflow.ErrVersionDowngrade) || deliverer.calls != 0 {
		t.Fatalf("err=%v deliveries=%d", err, deliverer.calls)
	}
}

func TestFlowDryRunDoesNotApplyAndReturnsCopyPlan(t *testing.T) {
	deliverer := &fakeDeliverer{copyResult: true}
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/acme/web:v1", UpstreamRef: "ghcr.io/acme/web:v1.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			t.Fatal("manifest applied during dry-run")
			return nil, nil
		},
	}
	config := imageConfig()
	config.Images = config.Images[1:]
	result, err := flow.Check(context.Background(), imageflow.Request{Config: config, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || deliverer.last.DryRun != true || result.Images[0].Copied {
		t.Fatalf("result=%#v request=%#v", result, deliverer.last)
	}
}

func TestFlowReturnsStructuredLazyCatCopyResult(t *testing.T) {
	deliverer := &fakeDeliverer{copyResult: true}
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/acme/web:v1", UpstreamRef: "ghcr.io/acme/web:v1.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Delivery.Mode = "lazycat"
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	image := result.Images[0]
	if !image.Copied || image.CopyResult == nil || image.CopyResult.Platform != "amd64" || !image.CopyResult.Finished {
		t.Fatalf("image=%#v", image)
	}
}

func TestFlowHandlesConcurrentProgressCallbacks(t *testing.T) {
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}}}},
		Deliverer: concurrentProgressDeliverer{},
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/acme/web:v1", UpstreamRef: "ghcr.io/acme/web:v1.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Delivery.Mode = "lazycat"
	result, err := flow.Check(t.Context(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Images) != 1 || !result.Images[0].Copied {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowRefreshesMutableCreatedLazyCatImageAndRemainsIdempotent(t *testing.T) {
	candidate := versioning.Candidate{Tag: "nightly", Digest: "sha256:a1b2c3d4e5f6" + strings.Repeat("0", 52), Created: time.Date(2026, 7, 10, 15, 30, 20, 0, time.UTC)}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Channel = "nightly"
	cfg.Images[0].Sort = "created"
	cfg.Images[0].TagRegex = "^nightly$"
	cfg.Images[0].Delivery.Mode = "lazycat"
	cfg.Update.AllowDowngrade = true
	newVersion := "0.0.0-nightly.20260710153020.a1b2c3d4e5f6"

	tests := []struct {
		name           string
		projectVersion string
		currentRuntime string
		wantChanged    bool
	}{
		{name: "new digest address", projectVersion: "1.0.0", currentRuntime: "registry.lazycat.cloud/web:old-copy", wantChanged: true},
		{name: "same digest address", projectVersion: newVersion, currentRuntime: "registry.lazycat.cloud/web:nightly", wantChanged: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deliverer := &fakeDeliverer{copyResult: true}
			applied := 0
			flow := imageflow.Flow{
				Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {candidate}}},
				Deliverer: deliverer,
				ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
					return []manifestedit.Current{{ID: "web", RuntimeRef: test.currentRuntime, UpstreamRef: "ghcr.io/acme/web:nightly"}}, nil
				},
				ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
					applied++
					return []manifestedit.Change{{ID: "web", Changed: true}}, nil
				},
			}
			result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: test.projectVersion}})
			if err != nil {
				t.Fatal(err)
			}
			if deliverer.calls != 1 || result.Changed != test.wantChanged {
				t.Fatalf("deliveries=%d result=%#v", deliverer.calls, result)
			}
			wantApplied := 0
			if test.wantChanged {
				wantApplied = 1
			}
			if applied != wantApplied {
				t.Fatalf("applied=%d wantChanged=%t", applied, test.wantChanged)
			}
		})
	}
}

func TestFlowBumpsPatchOnlyWhenMutableDigestChanges(t *testing.T) {
	candidate := versioning.Candidate{Tag: "latest", Digest: digest("b"), Created: created(12)}
	tests := []struct {
		name          string
		digestChanged bool
		wantVersion   string
		wantChanged   bool
	}{
		{name: "changed digest bumps once", digestChanged: true, wantVersion: "1.4.7", wantChanged: true},
		{name: "equal digest is no-op", digestChanged: false, wantVersion: "1.4.6", wantChanged: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := mutableImageConfig()
			deliverer := &fakeDeliverer{digestChanged: test.digestChanged, currentDigest: digest("a")}
			applied := 0
			var appliedUpdate manifestedit.Update
			flow := imageflow.Flow{
				Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {candidate}}},
				Deliverer: deliverer,
				ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
					return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:current", UpstreamRef: "ghcr.io/acme/web:latest@" + digest("a")}}, nil
				},
				ApplyManifest: func(_ string, updates []manifestedit.Update) ([]manifestedit.Change, error) {
					applied++
					appliedUpdate = updates[0]
					return []manifestedit.Change{{ID: "web", Changed: true}}, nil
				},
			}
			result, err := flow.Check(t.Context(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.4.6"}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Version != test.wantVersion || result.Changed != test.wantChanged || deliverer.last.CurrentRef != "registry.lazycat.cloud/web:current" || deliverer.last.CurrentDigest != digest("a") || !deliverer.last.Mutable {
				t.Fatalf("result=%#v delivery=%#v", result, deliverer.last)
			}
			wantApplied := 0
			if test.wantChanged {
				wantApplied = 1
			}
			if applied != wantApplied {
				t.Fatalf("applied=%d", applied)
			}
			if test.wantChanged && appliedUpdate.SourceRef != "ghcr.io/acme/web:latest@"+candidate.Digest {
				t.Fatalf("applied update=%#v", appliedUpdate)
			}
			image := result.Images[0]
			if image.Bump != "patch" || image.PreviousVersion != "1.4.6" || image.SelectedVersion != test.wantVersion || image.DigestChanged != test.digestChanged {
				t.Fatalf("image=%#v", image)
			}
			encoded, err := json.Marshal(image)
			if err != nil || !bytes.Contains(encoded, []byte(`"digestChanged":`)) {
				t.Fatalf("encoded=%s err=%v", encoded, err)
			}
		})
	}
}

func TestFlowMutableDryRunCalculatesBumpWithoutApplying(t *testing.T) {
	cfg := mutableImageConfig()
	deliverer := &fakeDeliverer{digestChanged: true, currentDigest: digest("a")}
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "latest", Digest: digest("b"), Created: created(12)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:current", UpstreamRef: "ghcr.io/acme/web:latest@" + digest("a")}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			t.Fatal("dry-run applied manifest")
			return nil, nil
		},
	}
	result, err := flow.Check(t.Context(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.4.6"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "1.4.7" || !deliverer.last.DryRun {
		t.Fatalf("result=%#v request=%#v", result, deliverer.last)
	}
}

func TestFlowMutableDeliveryMigrationKeepsVersionWhenDigestMatches(t *testing.T) {
	cfg := mutableImageConfig()
	deliverer := &fakeDeliverer{currentDigest: digest("a"), deliveryChanged: true}
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "latest", Digest: digest("a"), Created: created(12)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "docker.1ms.run/acme/web:latest", UpstreamRef: "ghcr.io/acme/web:latest"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(t.Context(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.4.6"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "1.4.6" || result.Images[0].DigestChanged {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowValidatesManifestTargetsBeforeRegistryCalls(t *testing.T) {
	registryClient := &fakeRegistry{}
	flow := imageflow.Flow{
		Registry:  registryClient,
		Deliverer: &fakeDeliverer{},
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return nil, errors.New("service missing")
		},
	}
	if _, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), Project: project.Info{ManifestFile: "manifest.yml"}}); err == nil {
		t.Fatal("expected target validation failure")
	}
	if registryClient.calls != 0 {
		t.Fatalf("registry calls=%d", registryClient.calls)
	}
}

func TestFlowRejectsUnknownImageID(t *testing.T) {
	flow := imageflow.Flow{Registry: &fakeRegistry{}, Deliverer: &fakeDeliverer{}}
	if _, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), ImageID: "missing"}); err == nil {
		t.Fatal("expected unknown image ID failure")
	}
}

func TestFlowDoesNotMisclassifyRegistryAuthenticationFailureAsPlatformMissing(t *testing.T) {
	flow := imageflow.Flow{
		Registry:  errorRegistry{err: errors.New("unauthorized")},
		Deliverer: &fakeDeliverer{},
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "old", UpstreamRef: "old"}}, nil
		},
	}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	_, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml"}})
	if err == nil || errors.Is(err, imageflow.ErrPlatformNotFound) || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("err=%v", err)
	}
}

func TestFlowClassifiesOnlyActualMirrorVerificationFailures(t *testing.T) {
	for _, test := range []struct {
		name        string
		deliveryErr error
		wantMirror  bool
	}{
		{name: "verification", deliveryErr: fmt.Errorf("%w: timeout", delivery.ErrMirrorVerification), wantMirror: true},
		{name: "configuration", deliveryErr: errors.New("mirror image template is required")},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := imageConfig()
			cfg.Images = cfg.Images[1:]
			cfg.Images[0].Delivery = config.Delivery{Mode: "mirror", ImageTemplate: "mirror.example/acme/web:{tag}", RequireDigestMatch: true}
			flow := imageflow.Flow{
				Registry: &fakeRegistry{bySource: map[string][]versioning.Candidate{
					"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("a"), Created: created(1)}},
				}},
				Deliverer: &fakeDeliverer{err: test.deliveryErr},
				ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
					return []manifestedit.Current{{ID: "web", UpstreamRef: "ghcr.io/acme/web:v1.0.0", RuntimeRef: "mirror.example/acme/web:v1.0.0"}}, nil
				},
			}
			_, err := flow.Check(t.Context(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
			if err == nil || errors.Is(err, imageflow.ErrMirrorVerificationFailed) != test.wantMirror {
				t.Fatalf("err=%v wantMirror=%t", err, test.wantMirror)
			}
		})
	}
}

func TestFlowFixtureUpdatesExplicitWebServiceOnly(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "image-app")
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "testdata", "image-app"))); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".github", "lazycat-action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Project.Root = root
	info, err := project.Inspect(ctx, cfg.Project)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(info.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	flow := imageflow.Flow{
		Registry: &fakeRegistry{bySource: map[string][]versioning.Candidate{
			"ghcr.io/acme/example-web": {{Tag: "v2.0.0", Digest: digest("f"), Created: created(10)}},
		}},
		Deliverer: &fakeDeliverer{copyResult: true},
	}
	result, err := flow.Check(ctx, imageflow.Request{Config: cfg, Project: info})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(info.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "2.0.0" || len(result.Images) != 1 || result.Images[0].ID != "web" {
		t.Fatalf("result=%#v", result)
	}
	if strings.Count(string(before), "image: postgres:17") != 1 || strings.Count(string(after), "image: postgres:17") != 1 {
		t.Fatalf("database service changed:\n%s", after)
	}
	if strings.Contains(string(after), "registry.lazycat.cloud/acme/example-web:old") || !strings.Contains(string(after), "# upstream: ghcr.io/acme/example-web:v2.0.0") {
		t.Fatalf("web service was not updated as expected:\n%s", after)
	}
}

type fakeRegistry struct {
	bySource   map[string][]versioning.Candidate
	calls      int
	lastFilter registry.TagFilter
	target     platform.Target
}

type errorRegistry struct{ err error }

func (registryClient errorRegistry) CandidatesForTarget(context.Context, string, platform.Target, ...registry.TagFilter) ([]versioning.Candidate, error) {
	return nil, registryClient.err
}

func (registryClient *fakeRegistry) CandidatesForTarget(_ context.Context, source string, target platform.Target, filters ...registry.TagFilter) ([]versioning.Candidate, error) {
	registryClient.calls++
	registryClient.target = target
	result := append([]versioning.Candidate(nil), registryClient.bySource[source]...)
	if len(filters) == 0 {
		return result, nil
	}
	registryClient.lastFilter = filters[0]
	filtered := result[:0]
	for _, candidate := range result {
		if filters[0].Include != nil && !filters[0].Include.MatchString(candidate.Tag) {
			continue
		}
		if filters[0].Exclude != nil && filters[0].Exclude.MatchString(candidate.Tag) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered, nil
}

type fakeDeliverer struct {
	calls           int
	last            delivery.Request
	copyResult      bool
	digestChanged   bool
	deliveryChanged bool
	currentDigest   string
	err             error
}

type concurrentProgressDeliverer struct{}

func (concurrentProgressDeliverer) Deliver(_ context.Context, request delivery.Request) (delivery.Result, error) {
	var wg sync.WaitGroup
	for layer := range 8 {
		wg.Go(func() {
			for progress := 0; progress <= 100; progress += 25 {
				request.OnProgress(appstore.CopyProgress{Layers: []appstore.LayerProgress{{Hash: fmt.Sprintf("sha256:%02d", layer), Progress: progress}}})
			}
		})
	}
	wg.Wait()
	request.OnProgress(appstore.CopyProgress{Finished: true})
	runtimeRef := "registry.lazycat.cloud/" + request.Image.ID + ":" + request.Tag
	copyResult := appstore.CopyImageResult{
		SourceImage: request.SourceRef, Platform: request.Target.Arch, LazyCatImage: runtimeRef,
		Progress: appstore.CopyProgress{Finished: true},
	}
	return delivery.Result{Mode: "lazycat", RuntimeRef: runtimeRef, Copied: true, CopyResult: &copyResult}, nil
}

func (deliverer *fakeDeliverer) Deliver(_ context.Context, request delivery.Request) (delivery.Result, error) {
	deliverer.calls++
	deliverer.last = request
	if deliverer.err != nil {
		return delivery.Result{}, deliverer.err
	}
	runtime := request.SourceRef
	if request.Image.Delivery.Mode == "lazycat" {
		runtime = "registry.lazycat.cloud/" + request.Image.ID + ":" + request.Tag
	}
	result := delivery.Result{Mode: request.Image.Delivery.Mode, RuntimeRef: runtime, CurrentDigest: deliverer.currentDigest, DigestChanged: deliverer.digestChanged, DeliveryChanged: deliverer.deliveryChanged}
	if deliverer.copyResult && !request.DryRun {
		copyResult := appstore.CopyImageResult{SourceImage: request.SourceRef, Platform: request.Target.Arch, LazyCatImage: runtime, Progress: appstore.CopyProgress{Finished: true}}
		result.Copied = true
		result.CopyResult = &copyResult
	}
	return result, nil
}

func mutableImageConfig() config.Config {
	return config.Config{
		Update: config.Update{Strategy: config.StrategyPublish, VersionSource: config.VersionSource{Type: config.VersionSourceImage, Image: "web", Bump: "patch"}},
		Images: []config.Image{{
			ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web",
			Channel: "custom", Sort: "created", TagRegex: "^latest$", VersionTemplate: "{version}", Delivery: config.Delivery{Mode: "lazycat"},
		}},
	}
}

func imageConfig() config.Config {
	return config.Config{
		Update: config.Update{VersionSource: config.VersionSource{Type: config.VersionSourceImage, Image: "web"}},
		Images: []config.Image{
			{ID: "db", Target: "service", Service: "db", Source: "docker.io/library/postgres", Channel: "stable", Sort: "semver", VersionTemplate: "{version}", Delivery: config.Delivery{Mode: "direct"}},
			{ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver", VersionTemplate: "{version}", Delivery: config.Delivery{Mode: "direct"}},
		},
	}
}

func digest(character string) string { return "sha256:" + strings.Repeat(character, 64) }
func created(day int) time.Time      { return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC) }
