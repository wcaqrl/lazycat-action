package publishflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	officialcatalog "github.com/lib-x/lzc-toolkit-go/appstore/official"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	"github.com/wcaqrl/lazycat-action/internal/storelookup"
)

var (
	ErrPublishStrategyRequired = errors.New("publish strategy is required")
	ErrStoreDisabled           = errors.New("requested store is disabled")
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const (
	skipReasonVersionAlreadyOnline = "version-already-online"
	skipReasonOnlineVersionNewer   = "online-version-newer"
)

type Request struct {
	Config              config.Config
	Project             project.Info
	LPKPath             string
	Version             string
	Changelog           string
	ExpectedSHA256      string
	GuardOfficialReview bool
	DryRun              bool
}

type Result struct {
	Artifact lpkcheck.Result  `json:"-"`
	Official *official.Result `json:"official,omitempty"`
}

type Flow struct {
	Verify            func(context.Context, lpkcheck.Request) (lpkcheck.Result, error)
	PrecheckOfficial  func(context.Context, string) error
	ResolveAuth       func(context.Context) (platformauth.Result, error)
	PublishOfficial   func(context.Context, official.Request) (official.Result, error)
	ConfigureOfficial func(platformauth.Result) official.Publisher
	LookupVersion     storelookup.Lookup
	LookupEnv         func(string) (string, bool)
	Logger            *slog.Logger
}

func Default() Flow {
	resolver := platformauth.Resolver{}
	return Flow{
		Verify:            lpkcheck.File,
		PrecheckOfficial:  official.PrecheckFile,
		ResolveAuth:       resolver.Resolve,
		ConfigureOfficial: platformPublisher,
		LookupVersion:     storelookup.Default,
		LookupEnv:         os.LookupEnv,
	}
}

func platformPublisher(resolved platformauth.Result) official.Publisher {
	if resolved.Protocol == platformauth.ProtocolLegacySession {
		return official.Publisher{}
	}
	return official.Publisher{
		BaseURL: resolved.BaseURL, SDK: true,
	}
}

func (flow Flow) Publish(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("publish stores: context is required")
	}
	logger := flow.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	logger.Info("official store publication started", "package", request.Project.PackageID, "version", request.Version, "dry_run", request.DryRun)
	if request.Config.Update.Strategy != config.StrategyPublish {
		return Result{}, ErrPublishStrategyRequired
	}
	if !request.Config.Stores.Official.Enabled {
		return Result{}, ErrStoreDisabled
	}
	version := strings.TrimSpace(request.Version)
	if version == "" {
		version = strings.TrimSpace(request.Project.Version)
	}
	if strings.TrimSpace(request.LPKPath) == "" || version == "" {
		return Result{}, errors.New("publish stores: LPK path and version are required")
	}
	verify := flow.Verify
	if verify == nil {
		return Result{}, errors.New("publish stores: LPK verifier is unavailable")
	}
	target := request.Config.Project.Target()
	artifact, err := verify(ctx, lpkcheck.Request{
		ProjectRoot: request.Project.Root, Path: request.LPKPath,
		ExpectedPackageID: request.Project.PackageID, ExpectedVersion: version,
		Target: target,
	})
	if err != nil {
		return Result{}, fmt.Errorf("verify publish artifact: %w", err)
	}
	logger.Info("LPK publication artifact verified", "store", "official", "package", artifact.PackageID, "version", artifact.Version, "size_bytes", artifact.Size, "sha256", artifact.SHA256)
	if artifact.TargetPlatform != target.Platform() {
		return Result{}, fmt.Errorf("verify publish artifact: target %q does not match %q", artifact.TargetPlatform, target.Platform())
	}
	expectedSHA256 := strings.ToLower(strings.TrimSpace(request.ExpectedSHA256))
	if expectedSHA256 != "" {
		if !sha256Pattern.MatchString(expectedSHA256) {
			return Result{}, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "publishflow.verify", Cause: errors.New("expected LPK SHA256 must be 64 lowercase hexadecimal characters")}
		}
		if artifact.SHA256 != expectedSHA256 {
			return Result{}, &lpkgo.Error{Code: lpkgo.CodeIntegrityMismatch, Op: "publishflow.verify", Cause: errors.New("local LPK SHA256 does not match the expected SHA256")}
		}
	}
	result := Result{Artifact: artifact}
	onlineVersion, skipReason, err := flow.checkExisting(ctx, request, artifact)
	if err != nil {
		return Result{}, err
	}
	if skipReason != "" {
		logger.Info("official store publication skipped", "candidate_version", artifact.Version, "online_version", onlineVersion, "skip_reason", skipReason)
		result.Official = &official.Result{
			Skipped: true, PackageID: artifact.PackageID, Version: artifact.Version,
			OnlineVersion: onlineVersion, SkipReason: skipReason, SHA256: artifact.SHA256,
		}
		return result, nil
	}
	if flow.PrecheckOfficial == nil {
		return Result{}, errors.New("official precheck dependency is unavailable")
	}
	if err := flow.PrecheckOfficial(ctx, artifact.Path); err != nil {
		return Result{}, fmt.Errorf("precheck official publish artifact: %w", err)
	}
	published, publishErr := flow.publishOfficial(ctx, request, result, onlineVersion, logger)
	if publishErr == nil {
		logger.Info("official store publication completed", "package", artifact.PackageID, "version", artifact.Version)
	}
	return published, publishErr
}

func (flow Flow) checkExisting(ctx context.Context, request Request, artifact lpkcheck.Result) (string, string, error) {
	if request.DryRun {
		return "", "", nil
	}
	if !request.Config.Stores.Official.SkipIfVersionExists {
		return "", "", nil
	}
	lookup := flow.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	baseURL, err := officialcatalog.MetadataBaseURL(envValue(lookup, "LZC_APPSTORE_COS_DOMAIN"))
	if err != nil {
		return "", "", fmt.Errorf("configure official store metadata: %w", err)
	}
	lookupRequest := storelookup.Request{Store: storelookup.StoreOfficial, PackageID: artifact.PackageID, BaseURL: baseURL}
	if flow.LookupVersion == nil {
		return "", "", errors.New("store version lookup dependency is unavailable")
	}
	lookupResult, err := flow.LookupVersion(ctx, lookupRequest)
	if err != nil {
		if errors.Is(err, lpkgo.ErrNotFound) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("query official store latest version: %w", err)
	}
	onlineVersion := strings.TrimSpace(lookupResult.OnlineVersion)
	if onlineVersion == artifact.Version {
		return onlineVersion, skipReasonVersionAlreadyOnline, nil
	}
	if !request.Config.Update.AllowDowngrade && newerSemVer(onlineVersion, artifact.Version) {
		return onlineVersion, skipReasonOnlineVersionNewer, nil
	}
	return onlineVersion, "", nil
}

func newerSemVer(onlineVersion, candidateVersion string) bool {
	online, onlineErr := semver.NewVersion(strings.TrimSpace(onlineVersion))
	candidate, candidateErr := semver.NewVersion(strings.TrimSpace(candidateVersion))
	return onlineErr == nil && candidateErr == nil && online.GreaterThan(candidate)
}

func (flow Flow) publishOfficial(ctx context.Context, request Request, result Result, onlineVersion string, logger *slog.Logger) (Result, error) {
	changelog := strings.TrimSpace(request.Changelog)
	if changelog == "" {
		return Result{}, errors.New("official publishing requires a changelog")
	}
	input := official.Request{
		ProjectRoot: request.Project.Root, LPKPath: result.Artifact.Path, FileName: filepath.Base(result.Artifact.Path),
		PackageID: result.Artifact.PackageID, Version: result.Artifact.Version, SHA256: result.Artifact.SHA256,
		Changelog: changelog, Locales: request.Config.Stores.Official.Locales,
		CreateIfMissing: request.Config.Stores.Official.CreateIfMissing,
		Application:     request.Config.Stores.Official.Application, DefaultName: request.Project.Name,
		Retry: request.Config.Stores.Official.Retry, Logger: logger,
		GuardPendingReview:     request.GuardOfficialReview,
		ContinueIfNewerVersion: request.Config.Update.VersionSource.Type == config.VersionSourceImage && request.Config.Stores.Official.ShouldContinueIfNewerVersion(),
	}
	if request.DryRun {
		result.Official = &official.Result{PackageID: input.PackageID, Version: input.Version, SHA256: input.SHA256}
		return result, nil
	}
	if flow.ResolveAuth == nil || flow.PublishOfficial == nil && flow.ConfigureOfficial == nil {
		return Result{}, errors.New("official publisher dependencies are unavailable")
	}
	resolved, err := flow.ResolveAuth(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("resolve official credentials: %w", err)
	}
	input.Provider = resolved.Provider
	publish := flow.PublishOfficial
	if flow.ConfigureOfficial != nil {
		publish = flow.ConfigureOfficial(resolved).Publish
	}
	published, err := publish(ctx, input)
	if err != nil {
		return Result{}, fmt.Errorf("publish official store: %w", err)
	}
	result.Official = &published
	result.Official.OnlineVersion = onlineVersion
	return result, nil
}

func envValue(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}
