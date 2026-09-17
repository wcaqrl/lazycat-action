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
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	private "github.com/wcaqrl/lazycat-action/internal/store/private"
	"github.com/wcaqrl/lazycat-action/internal/storelookup"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	officialcatalog "github.com/lib-x/lzc-toolkit-go/appstore/official"
)

var (
	ErrPublishStrategyRequired = errors.New("publish strategy is required")
	ErrStoreDisabled           = errors.New("requested store is disabled")
	ErrReleaseAssetMissing     = errors.New("private publishing requires a GitHub Release Asset URL and confirmed SHA256")
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Target string

const (
	TargetOfficial Target = "official"
	TargetPrivate  Target = "private"

	skipReasonVersionAlreadyOnline = "version-already-online"
	skipReasonOnlineVersionNewer   = "online-version-newer"
)

type Request struct {
	Target              Target
	Config              config.Config
	Project             project.Info
	LPKPath             string
	Version             string
	Changelog           string
	DownloadURL         string
	ExpectedSHA256      string
	GuardOfficialReview bool
	DryRun              bool
}

type Result struct {
	Artifact lpkcheck.Result  `json:"-"`
	Official *official.Result `json:"official,omitempty"`
	Private  *private.Result  `json:"private,omitempty"`
}

type PrivatePublisher interface {
	Publish(context.Context, private.Request) (private.Result, error)
}

type Flow struct {
	Verify            func(context.Context, lpkcheck.Request) (lpkcheck.Result, error)
	PrecheckOfficial  func(context.Context, string) error
	ResolveAuth       func(context.Context) (platformauth.Result, error)
	PublishOfficial   func(context.Context, official.Request) (official.Result, error)
	ConfigureOfficial func(platformauth.Result) official.Publisher
	LookupVersion     storelookup.Lookup
	LookupEnv         func(string) (string, bool)
	NewPrivate        func(private.Options) (PrivatePublisher, error)
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
		NewPrivate: func(options private.Options) (PrivatePublisher, error) {
			return private.New(options)
		},
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
	logger.Info("store publication started", "store", request.Target, "package", request.Project.PackageID, "version", request.Version, "dry_run", request.DryRun)
	if request.Config.Update.Strategy != config.StrategyPublish {
		return Result{}, ErrPublishStrategyRequired
	}
	switch request.Target {
	case TargetOfficial:
		if !request.Config.Stores.Official.Enabled {
			return Result{}, ErrStoreDisabled
		}
	case TargetPrivate:
		if !request.Config.Stores.Private.Enabled {
			return Result{}, ErrStoreDisabled
		}
		if strings.TrimSpace(request.DownloadURL) == "" {
			return Result{}, ErrReleaseAssetMissing
		}
		if strings.TrimSpace(request.ExpectedSHA256) == "" {
			return Result{}, ErrReleaseAssetMissing
		}
	default:
		return Result{}, fmt.Errorf("unsupported publish target %q", request.Target)
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
	logger.Info("LPK publication artifact verified", "store", request.Target, "package", artifact.PackageID, "version", artifact.Version, "size_bytes", artifact.Size, "sha256", artifact.SHA256)
	if artifact.TargetPlatform != target.Platform() {
		return Result{}, fmt.Errorf("verify publish artifact: target %q does not match %q", artifact.TargetPlatform, target.Platform())
	}
	expectedSHA256 := strings.ToLower(strings.TrimSpace(request.ExpectedSHA256))
	if expectedSHA256 != "" {
		if !sha256Pattern.MatchString(expectedSHA256) {
			return Result{}, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "publishflow.verify", Cause: errors.New("expected Release Asset SHA256 must be 64 lowercase hexadecimal characters")}
		}
		if artifact.SHA256 != expectedSHA256 {
			return Result{}, &lpkgo.Error{Code: lpkgo.CodeIntegrityMismatch, Op: "publishflow.verify", Cause: errors.New("local LPK SHA256 does not match the confirmed Release Asset SHA256")}
		}
	}
	result := Result{Artifact: artifact}
	onlineVersion, skipReason, err := flow.checkExisting(ctx, request, artifact)
	if err != nil {
		return Result{}, err
	}
	if skipReason != "" {
		logger.Info("store publication skipped", "store", request.Target, "candidate_version", artifact.Version, "online_version", onlineVersion, "skip_reason", skipReason)
		switch request.Target {
		case TargetOfficial:
			result.Official = &official.Result{
				Skipped: true, PackageID: artifact.PackageID, Version: artifact.Version,
				OnlineVersion: onlineVersion, SkipReason: skipReason, SHA256: artifact.SHA256,
			}
		case TargetPrivate:
			result.Private = &private.Result{
				Skipped: true, PackageID: artifact.PackageID, Version: artifact.Version,
				OnlineVersion: onlineVersion, SkipReason: skipReason, DownloadURL: strings.TrimSpace(request.DownloadURL), SHA256: artifact.SHA256,
			}
		}
		return result, nil
	}
	if request.Target == TargetOfficial {
		if flow.PrecheckOfficial == nil {
			return Result{}, errors.New("official precheck dependency is unavailable")
		}
		if err := flow.PrecheckOfficial(ctx, artifact.Path); err != nil {
			return Result{}, fmt.Errorf("precheck official publish artifact: %w", err)
		}
	}
	switch request.Target {
	case TargetOfficial:
		published, publishErr := flow.publishOfficial(ctx, request, result, onlineVersion, logger)
		if publishErr == nil {
			logger.Info("store publication completed", "store", request.Target, "package", artifact.PackageID, "version", artifact.Version)
		}
		return published, publishErr
	case TargetPrivate:
		published, publishErr := flow.publishPrivate(ctx, request, result, onlineVersion)
		if publishErr == nil {
			logger.Info("store publication completed", "store", request.Target, "package", artifact.PackageID, "version", artifact.Version)
		}
		return published, publishErr
	default:
		panic("validated publish target became invalid")
	}
}

func (flow Flow) checkExisting(ctx context.Context, request Request, artifact lpkcheck.Result) (string, string, error) {
	if request.DryRun {
		return "", "", nil
	}
	enabled := false
	lookupRequest := storelookup.Request{PackageID: artifact.PackageID}
	switch request.Target {
	case TargetOfficial:
		enabled = request.Config.Stores.Official.SkipIfVersionExists
		lookupRequest.Store = storelookup.StoreOfficial
	case TargetPrivate:
		enabled = request.Config.Stores.Private.SkipIfVersionExists
		lookupRequest.Store = storelookup.StorePrivate
		lookup := flow.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		lookupRequest.BaseURL = envValue(lookup, "APPSTORE_URL")
		lookupRequest.GroupCodes = commaSeparated(envValue(lookup, "PRIVATE_STORE_GROUP_CODES"))
	}
	if !enabled {
		return "", "", nil
	}
	if request.Target == TargetOfficial {
		lookup := flow.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		baseURL, err := officialcatalog.MetadataBaseURL(envValue(lookup, "LZC_APPSTORE_COS_DOMAIN"))
		if err != nil {
			return "", "", fmt.Errorf("configure official store metadata: %w", err)
		}
		lookupRequest.BaseURL = baseURL
	}
	if flow.LookupVersion == nil {
		return "", "", errors.New("store version lookup dependency is unavailable")
	}
	lookupResult, err := flow.LookupVersion(ctx, lookupRequest)
	if err != nil {
		if errors.Is(err, lpkgo.ErrNotFound) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("query %s store latest version: %w", request.Target, err)
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

func (flow Flow) publishPrivate(ctx context.Context, request Request, result Result, onlineVersion string) (Result, error) {
	lookup := flow.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	appID := envValue(lookup, "APP_ID")
	name := strings.TrimSpace(request.Config.Stores.Private.Name)
	if name == "" {
		name = strings.TrimSpace(request.Project.Name)
	}
	if name == "" {
		name = result.Artifact.PackageID
	}
	summary := strings.TrimSpace(request.Config.Stores.Private.Summary)
	if summary == "" {
		summary = strings.TrimSpace(request.Project.Description)
	}
	if summary == "" {
		summary = name
	}
	input := private.Request{
		AppID: appID, PackageID: result.Artifact.PackageID, Name: name, Summary: summary,
		Version: result.Artifact.Version, Changelog: strings.TrimSpace(request.Changelog),
		DownloadURL: strings.TrimSpace(request.DownloadURL), SHA256: result.Artifact.SHA256,
	}
	if request.DryRun {
		result.Private = &private.Result{
			AppID: appID, PackageID: input.PackageID, Version: input.Version,
			DownloadURL: input.DownloadURL, SHA256: input.SHA256,
		}
		return result, nil
	}
	baseURL := envValue(lookup, "APPSTORE_URL")
	token := envValue(lookup, "APPSTORE_TOKEN")
	if baseURL == "" || token == "" {
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Op: "publishflow.private", Cause: errors.New("APPSTORE_URL and APPSTORE_TOKEN are required")}
	}
	if flow.NewPrivate == nil {
		return Result{}, errors.New("private publisher dependency is unavailable")
	}
	client, err := flow.NewPrivate(private.Options{BaseURL: baseURL, Token: token})
	if err != nil {
		return Result{}, fmt.Errorf("configure private store: %w", err)
	}
	published, err := client.Publish(ctx, input)
	if err != nil {
		return Result{}, fmt.Errorf("publish private store: %w", err)
	}
	result.Private = &published
	result.Private.OnlineVersion = onlineVersion
	return result, nil
}

func envValue(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

func commaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
