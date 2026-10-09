package action

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	actionbuild "github.com/wcaqrl/lazycat-action/internal/build"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/pipelinestate"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/prepare"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/publishflow"
	"github.com/wcaqrl/lazycat-action/internal/source"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	"github.com/wcaqrl/lazycat-action/internal/yamledit"
)

func reviewDependencies(t *testing.T, lock *pipelinestate.Lock, candidate source.Candidate) Dependencies {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{Version: 2, Project: config.Project{Root: root}, Source: config.Source{Kind: config.SourceKindGit}, State: config.State{File: ".lazycat-action.lock.yml"}, Update: config.Update{Strategy: config.StrategyPublish}, Stores: config.Stores{Official: config.OfficialStore{Enabled: true}}}
	return Dependencies{
		Host: platform.Host{OS: "linux", Arch: "amd64"}, ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "dev.example.app", Version: lock.Application.Version, PackageFile: filepath.Join(root, "package.yml")}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(_ context.Context, r actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{PackageID: "dev.example.app", Version: r.Version, Path: filepath.Join(root, "app.lpk"), SHA256: "abc", TargetPlatform: "linux/amd64"}, nil
		},
		ReadState:      func(string) (pipelinestate.Lock, error) { return *lock, nil },
		WriteState:     func(_ string, saved pipelinestate.Lock) error { *lock = saved; return nil },
		Fingerprint:    func(context.Context, config.Config, source.Candidate) (string, error) { return "changed-recipe", nil },
		DiscoverSource: func(context.Context, source.Request) (source.Candidate, error) { return candidate, nil },
		PrepareSource:  func(context.Context, prepare.Request) (prepare.Result, error) { return prepare.Result{}, nil },
	}
}

func rejectedLock() pipelinestate.Lock {
	candidate := source.Candidate{Kind: "git", Version: "1.4.2", Tag: "v1.4.2", Ref: "refs/tags/v1.4.2", Revision: "abcdef"}
	return pipelinestate.Lock{Source: candidate, Fingerprint: "old-recipe", Status: "submitted", Application: pipelinestate.Application{PackageID: "dev.example.app", Version: "1.4.2", LPKSHA256: "abc"}, Reviews: []pipelinestate.Review{{ID: 9, Source: candidate, Fingerprint: "old-recipe", ApplicationVersion: "1.4.2", Status: "rejected", Reason: "fix packaging"}}}
}

func TestRejectedSourceDoesNotRebuildEvenWhenRecipeChanges(t *testing.T) {
	lock := rejectedLock()
	deps := reviewDependencies(t, &lock, lock.Source)
	deps.PrepareSource = func(context.Context, prepare.Request) (prepare.Result, error) {
		t.Fatal("must not rebuild rejected source automatically")
		return prepare.Result{}, nil
	}
	result, err := Run(t.Context(), Input{Operation: OperationCheck}, deps)
	if err != nil || result.Changed || result.LPKPath != "" || result.ReviewStatus != "rejected" || result.Version != "1.4.2" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRejectedRetryRemainsResumableAfterSubmissionFailure(t *testing.T) {
	for _, manual := range []bool{false, true} {
		lock := rejectedLock()
		lock.Status = "packaged"
		lock.Fingerprint = "changed-recipe"
		lock.Application.Version = "1.4.3"
		deps := reviewDependencies(t, &lock, lock.Source)
		result, err := Run(t.Context(), Input{Operation: OperationCheck, RetryRejected: manual}, deps)
		if err != nil || !result.Changed || result.Version != "1.4.3" {
			t.Fatalf("manual=%t result=%#v err=%v", manual, result, err)
		}
	}
}

func TestRetryRejectedBumpsApplicationVersionAndPreservesHistory(t *testing.T) {
	lock := rejectedLock()
	deps := reviewDependencies(t, &lock, lock.Source)
	result, err := Run(t.Context(), Input{Operation: OperationCheck, RetryRejected: true}, deps)
	if err != nil || !result.Changed || result.Version != "1.4.3" || lock.Status != "packaged" || len(lock.Reviews) != 1 || lock.Reviews[0].Status != "rejected" || lock.PackagedAt.IsZero() {
		t.Fatalf("result=%#v lock=%#v err=%v", result, lock, err)
	}
}

func TestNewSourceCanPublishAfterPreviousRejection(t *testing.T) {
	lock := rejectedLock()
	candidate := source.Candidate{Kind: "git", Version: "1.4.4", Tag: "v1.4.4", Ref: "refs/tags/v1.4.4", Revision: "newrevision"}
	result, err := Run(t.Context(), Input{Operation: OperationCheck}, reviewDependencies(t, &lock, candidate))
	if err != nil || !result.Changed || result.Version != "1.4.4" || len(lock.Reviews) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRecoveryOfRejectedReviewPreventsRepeatSubmission(t *testing.T) {
	lock := rejectedLock()
	lock.Status, lock.Reviews = "packaged", nil
	lock.PackagedAt = time.Now().Add(-time.Hour)
	deps := reviewDependencies(t, &lock, lock.Source)
	deps.LookupReview = func(_ context.Context, q official.ReviewLookup) (*official.Review, error) {
		if q.Version != "1.4.2" || q.SHA256 != "abc" || q.StartedAt.IsZero() {
			t.Fatalf("lookup=%#v", q)
		}
		return &official.Review{ID: 17, Status: -1, Reason: "rejected after Git push failed", CreatedAt: lock.PackagedAt}, nil
	}
	deps.Publish = func(context.Context, publishflow.Request) (publishflow.Result, error) {
		t.Fatal("existing rejected review must not be submitted again")
		return publishflow.Result{}, nil
	}
	result, err := Run(t.Context(), Input{Operation: OperationPublishOfficial, Version: "1.4.2", ExpectedSHA256: "abc"}, deps)
	if err != nil || result.ReviewStatus != "rejected" || lock.Status != "submitted" || lock.Reviews[0].ID != 17 {
		t.Fatalf("result=%#v lock=%#v err=%v", result, lock, err)
	}
}

func TestReviewLookupFailureStopsBuild(t *testing.T) {
	lock := rejectedLock()
	lock.Reviews[0].Status = "waiting"
	deps := reviewDependencies(t, &lock, lock.Source)
	deps.LookupReview = func(context.Context, official.ReviewLookup) (*official.Review, error) {
		return nil, errors.New("network unavailable")
	}
	deps.PrepareSource = func(context.Context, prepare.Request) (prepare.Result, error) {
		t.Fatal("lookup failure must stop processing")
		return prepare.Result{}, nil
	}
	if _, err := Run(t.Context(), Input{Operation: OperationCheck}, deps); err == nil {
		t.Fatal("expected query failure")
	}
}

func TestReviewStatePollingUsesCreationTimeRatherThanLastCheck(t *testing.T) {
	lock := rejectedLock()
	record := &lock.Reviews[0]
	record.Status = "waiting"
	record.CreatedAt = time.Now().Add(-7 * 24 * time.Hour)
	record.CheckedAt = time.Now().Add(-time.Hour)
	deps := reviewDependencies(t, &lock, lock.Source)
	deps.LookupReview = func(_ context.Context, q official.ReviewLookup) (*official.Review, error) {
		if !q.CreatedAt.Equal(record.CreatedAt) || q.ID != 9 {
			t.Fatalf("query=%#v", q)
		}
		return &official.Review{ID: 9, Status: -1, Reason: "late rejection", CreatedAt: record.CreatedAt}, nil
	}
	result, err := Run(t.Context(), Input{Operation: OperationCheck}, deps)
	if err != nil || result.Changed || !result.StateChanged || result.ReviewStatus != "rejected" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestSubmissionStoresReviewMetadataAndOldRejections(t *testing.T) {
	lock := rejectedLock()
	lock.Status = "packaged"
	lock.Application.Version = "1.4.3"
	lock.Fingerprint = "new-recipe"
	lock.PackagedAt = time.Now().Add(-time.Minute)
	deps := reviewDependencies(t, &lock, lock.Source)
	deps.Publish = func(context.Context, publishflow.Request) (publishflow.Result, error) {
		return publishflow.Result{Artifact: lpkcheck.Result{PackageID: "dev.example.app", Version: "1.4.3", SHA256: "abc", TargetPlatform: "linux/amd64"}, Official: &official.Result{Published: true, Review: &official.Review{ID: 21, Status: 0, CreatedAt: time.Now().UTC()}}}, nil
	}
	result, err := Run(t.Context(), Input{Operation: OperationPublishOfficial, Version: "1.4.3", ExpectedSHA256: "abc"}, deps)
	if err != nil || result.ReviewStatus != "waiting" || len(lock.Reviews) != 2 || lock.Reviews[1].ID != 21 || lock.Reviews[1].CreatedAt.IsZero() || lock.Reviews[0].Status != "rejected" {
		t.Fatalf("result=%#v lock=%#v err=%v", result, lock, err)
	}
}

func TestMissingLegacyReviewIsNotScannedEveryDay(t *testing.T) {
	lock := rejectedLock()
	lock.Reviews = nil
	deps := reviewDependencies(t, &lock, lock.Source)
	lookups := 0
	deps.LookupReview = func(context.Context, official.ReviewLookup) (*official.Review, error) { lookups++; return nil, nil }
	for range 2 {
		result, err := Run(t.Context(), Input{Operation: OperationCheck}, deps)
		if err != nil || result.Changed {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	}
	if lookups != 1 || lock.Reviews[0].Status != "untracked" {
		t.Fatalf("lookups=%d lock=%#v", lookups, lock)
	}
}
