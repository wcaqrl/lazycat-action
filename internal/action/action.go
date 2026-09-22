package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	actionbuild "github.com/wcaqrl/lazycat-action/internal/build"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/delivery"
	"github.com/wcaqrl/lazycat-action/internal/diagnostic"
	"github.com/wcaqrl/lazycat-action/internal/httpx"
	"github.com/wcaqrl/lazycat-action/internal/imageflow"
	"github.com/wcaqrl/lazycat-action/internal/imagemirror"
	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
	"github.com/wcaqrl/lazycat-action/internal/pipelinestate"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	"github.com/wcaqrl/lazycat-action/internal/prepare"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/publishflow"
	"github.com/wcaqrl/lazycat-action/internal/registry"
	"github.com/wcaqrl/lazycat-action/internal/source"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	"github.com/wcaqrl/lazycat-action/internal/versioning"
	"github.com/wcaqrl/lazycat-action/internal/yamledit"
)

const (
	CodeConfigInvalid            = "CONFIG_INVALID"
	CodeProjectUnsupported       = "PROJECT_UNSUPPORTED"
	CodeVersionNotFound          = "VERSION_NOT_FOUND"
	CodeVersionDowngradeBlocked  = "VERSION_DOWNGRADE_BLOCKED"
	CodeBuildFailed              = "BUILD_FAILED"
	CodeLPKInvalid               = "LPK_INVALID"
	CodePlatformNotFound         = "PLATFORM_NOT_FOUND"
	CodeImageCopyFailed          = "IMAGE_COPY_FAILED"
	CodeMirrorVerificationFailed = "MIRROR_VERIFICATION_FAILED"
	CodeStoreAuthFailed          = "STORE_AUTH_FAILED"
	CodeStorePublishFailed       = "STORE_PUBLISH_FAILED"
)

type Operation string

const (
	OperationAuto            Operation = "auto"
	OperationCheck           Operation = "check"
	OperationBuild           Operation = "build"
	OperationPublishOfficial Operation = "publish-official"
)

type Input struct {
	Operation             Operation
	ConfigPath            string
	ImageID               string
	Version               string
	Tag                   string
	Channel               string
	Changelog             string
	LPKPath               string
	ExpectedSHA256        string
	EventName             string
	RefType               string
	RefName               string
	SourceDateEpoch       int64
	WorkflowToolchains    string
	WorkflowGoVersion     string
	WorkflowNodeVersion   string
	WorkflowRustToolchain string
	GuardOfficialReview   bool
	DryRun                bool
}

type Result struct {
	Operation             string          `json:"operation"`
	Changed               bool            `json:"changed"`
	PackageID             string          `json:"packageId"`
	PackageFile           string          `json:"packageFile"`
	ManifestFile          string          `json:"manifestFile"`
	Version               string          `json:"version"`
	Tag                   string          `json:"tag"`
	LPKPath               string          `json:"lpkPath"`
	SHA256                string          `json:"sha256"`
	ImageResults          json.RawMessage `json:"imageResults"`
	SourceResult          json.RawMessage `json:"sourceResult"`
	Changelog             string          `json:"changelog,omitempty"`
	Fingerprint           string          `json:"fingerprint,omitempty"`
	StateFile             string          `json:"stateFile,omitempty"`
	StoreResults          json.RawMessage `json:"storeResults"`
	UpdateStrategy        string          `json:"updateStrategy"`
	OfficialStoreEnabled  bool            `json:"officialStoreEnabled"`
	OfficialReviewPending bool            `json:"officialReviewPending"`
	OfficialReviewVersion string          `json:"officialReviewVersion,omitempty"`
	Channel               string          `json:"channel,omitempty"`
	ResultFile            string          `json:"resultFile"`
	RunnerArch            string          `json:"runnerArch"`
	TargetPlatform        string          `json:"targetPlatform"`
	Warnings              []lpkgo.Warning `json:"warnings,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	message := err.Code + ": " + err.Message
	details := make([]string, 0, 5)
	var toolkitError *lpkgo.Error
	if errors.As(err.Cause, &toolkitError) {
		if toolkitError.Code != "" {
			details = append(details, "upstream="+string(toolkitError.Code))
		}
		if toolkitError.StatusCode > 0 {
			details = append(details, fmt.Sprintf("status=%d", toolkitError.StatusCode))
		}
		if toolkitError.Op != "" {
			details = append(details, "op="+toolkitError.Op)
		}
	}
	var publicPath interface{ PublicErrorPath() string }
	if errors.As(err.Cause, &publicPath) {
		if path := diagnostic.SafePath(publicPath.PublicErrorPath()); path != "" {
			details = append(details, "path="+quoteDiagnosticToken(path))
		}
	}
	var publicDetail interface{ PublicErrorDetail() string }
	if errors.As(err.Cause, &publicDetail) {
		if detail := diagnostic.SafeDetail(publicDetail.PublicErrorDetail()); detail != "" {
			details = append(details, "message="+strconv.Quote(detail))
		}
	}
	if len(details) == 0 {
		return message
	}
	return message + " (" + strings.Join(details, " ") + ")"
}

func quoteDiagnosticToken(value string) string {
	if strings.ContainsAny(value, " \t\r\n\"\\()") {
		return strconv.Quote(value)
	}
	return value
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

type Dependencies struct {
	Host                 platform.Host
	ResultDir            string
	Logger               *slog.Logger
	LoadConfig           func(string) (config.Config, error)
	Inspect              func(context.Context, config.Project) (project.Info, error)
	SetVersion           func(string, string) (yamledit.Change, error)
	Build                func(context.Context, actionbuild.Request) (actionbuild.Result, error)
	CheckImages          func(context.Context, imageflow.Request) (imageflow.Result, error)
	DiscoverSource       func(context.Context, source.Request) (source.Candidate, error)
	DiscoverChangelog    func(context.Context, config.Changelog, source.Candidate, source.Candidate, string, string) (string, error)
	PrepareSource        func(context.Context, prepare.Request) (prepare.Result, error)
	InspectPreparedImage func(context.Context, string, platform.Target) (registry.Image, error)
	DeliverPreparedImage func(context.Context, delivery.Request) (delivery.Result, error)
	ReadManifestImages   func(string, []manifestedit.Target) ([]manifestedit.Current, error)
	ApplyManifestImages  func(string, []manifestedit.Update) ([]manifestedit.Change, error)
	ReadState            func(string) (pipelinestate.Lock, error)
	WriteState           func(string, pipelinestate.Lock) error
	Fingerprint          func(context.Context, config.Config, source.Candidate) (string, error)
	WaitingReviewVersion func(context.Context, string) (string, bool, error)
	Publish              func(context.Context, publishflow.Request) (publishflow.Result, error)
}

func DefaultDependencies(host platform.Host) Dependencies {
	dependencies, err := DefaultDependenciesWithEnv(host, func(string) string { return "" })
	if err != nil {
		panic(err)
	}
	return dependencies
}

func DefaultDependenciesWithEnv(host platform.Host, getenv func(string) string) (Dependencies, error) {
	mirrors, err := imagemirror.FromEnvironment(getenv)
	if err != nil {
		return Dependencies{}, fmt.Errorf("configure image mirrors: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	builder := actionbuild.Builder{Logger: logger}
	registryClient := registry.New()
	sourceDiscoverer := source.Default(getenv)
	resolver := platformauth.Resolver{}
	storeClient := &platformImageCopier{resolver: resolver}
	deliveryResolver := delivery.Resolver{Copier: storeClient, Inspector: registryClient, Mirrors: mirrors}
	imageFlow := imageflow.Flow{
		Registry:     registryClient,
		Deliverer:    deliveryResolver,
		ResolveImage: deliveryResolver.ResolveImage,
		Logger:       logger,
	}
	publishFlow := publishflow.Default()
	publishFlow.Logger = logger
	return Dependencies{
		Host:              host,
		Logger:            logger,
		LoadConfig:        config.Load,
		Inspect:           project.Inspect,
		SetVersion:        yamledit.SetPackageVersion,
		Build:             builder.Build,
		CheckImages:       imageFlow.Check,
		DiscoverSource:    sourceDiscoverer.Discover,
		DiscoverChangelog: sourceDiscoverer.Git.Changelog,
		PrepareSource: prepare.Runner{
			Git: sourceDiscoverer.Git,
		}.Prepare,
		InspectPreparedImage: registryClient.InspectTarget,
		DeliverPreparedImage: deliveryResolver.Deliver,
		ReadManifestImages:   manifestedit.Read,
		ApplyManifestImages:  manifestedit.Apply,
		ReadState:            pipelinestate.Read,
		WriteState:           pipelinestate.Write,
		Fingerprint:          pipelinestate.Fingerprint,
		WaitingReviewVersion: func(ctx context.Context, packageID string) (string, bool, error) {
			resolved, err := resolver.Resolve(ctx)
			if err != nil {
				return "", false, err
			}
			client, err := platformStoreClient(resolved, appstore.Options{})
			if err != nil {
				return "", false, err
			}
			return client.WaitingReviewVersion(ctx, packageID)
		},
		Publish: publishFlow.Publish,
	}, nil
}

type platformImageCopier struct {
	resolver platformauth.Resolver
	mu       sync.Mutex
	client   *appstore.Client
}

func (copier *platformImageCopier) CopyImage(ctx context.Context, request appstore.CopyImageRequest) (appstore.CopyImageResult, error) {
	client, err := copier.clientFor(ctx)
	if err != nil {
		return appstore.CopyImageResult{}, err
	}
	return client.CopyImage(ctx, request)
}

func (copier *platformImageCopier) clientFor(ctx context.Context) (*appstore.Client, error) {
	copier.mu.Lock()
	defer copier.mu.Unlock()
	if copier.client != nil {
		return copier.client, nil
	}
	resolved, err := copier.resolver.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	copier.client, err = platformStoreClient(resolved, appstore.Options{})
	if err != nil {
		return nil, err
	}
	return copier.client, nil
}

func platformStoreClient(resolved platformauth.Result, options appstore.Options) (*appstore.Client, error) {
	options.Token = resolved.Provider
	if resolved.Protocol == platformauth.ProtocolPAT {
		options.BaseURL = resolved.BaseURL
		return appstore.NewPAT(options)
	}
	options.HTTPClient = httpx.NoRedirect(options.HTTPClient, 30*time.Second)
	return appstore.New(options), nil
}

func ResolveOperation(input Input, cfg config.Config) (Operation, error) {
	operation := input.Operation
	if operation == "" {
		operation = OperationAuto
	}
	if operation != OperationAuto {
		switch operation {
		case OperationCheck, OperationBuild, OperationPublishOfficial:
			return operation, nil
		default:
			return "", fmt.Errorf("unsupported operation %q", operation)
		}
	}
	if cfg.Version == 2 && input.Version == "" {
		return OperationCheck, nil
	}
	switch input.EventName {
	case "release":
		return OperationBuild, nil
	case "workflow_dispatch":
		if input.Version != "" || cfg.Update.VersionSource.Type == config.VersionSourceGit {
			return OperationBuild, nil
		}
		return OperationCheck, nil
	case "push":
		if input.RefType == "tag" || strings.HasPrefix(input.RefName, "v") {
			return OperationBuild, nil
		}
		return OperationBuild, nil
	case "schedule":
		return OperationCheck, nil
	default:
		return OperationBuild, nil
	}
}

func Run(ctx context.Context, input Input, dependencies Dependencies) (Result, error) {
	if ctx == nil {
		return Result{}, actionError(CodeConfigInvalid, "context is required", nil)
	}
	if err := validateDependencies(dependencies); err != nil {
		return Result{}, actionError(CodeConfigInvalid, "Action dependencies are incomplete", err)
	}
	if input.ConfigPath == "" {
		input.ConfigPath = ".github/lazycat-action.yml"
	}

	cfg, err := dependencies.LoadConfig(input.ConfigPath)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to load Action configuration", err)
	}
	requestedOperation := input.Operation
	if requestedOperation == "" {
		requestedOperation = OperationAuto
	}
	operation, err := ResolveOperation(input, cfg)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, err.Error(), err)
	}
	if err := validateWorkflowToolchains(input, cfg.Build.Toolchains); err != nil {
		return Result{}, actionError(CodeConfigInvalid, "workflow toolchains do not match Action configuration", err)
	}
	info, err := dependencies.Inspect(ctx, cfg.Project)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to inspect LazyCat project", err)
	}
	logger := dependencies.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	logger.Info("execution selected", "operation", operation, "mode", executionMode(operation, cfg), "package", info.PackageID, "version", info.Version, "project_kind", info.Kind, "target", cfg.Project.Target().Platform())
	officialReviewVersion := ""
	if shouldPauseForOfficialReview(input, requestedOperation, operation, cfg) {
		if dependencies.WaitingReviewVersion == nil {
			return Result{}, actionError(CodeConfigInvalid, "official waiting-review lookup dependency is unavailable", nil)
		}
		version, found, lookupErr := dependencies.WaitingReviewVersion(ctx, info.PackageID)
		if lookupErr != nil {
			return Result{}, mapWaitingReviewError(lookupErr)
		}
		if found {
			version = strings.TrimSpace(version)
			if version == "" {
				return Result{}, actionError(CodeConfigInvalid, "official waiting-review lookup returned an empty version", nil)
			}
			if operation == OperationCheck && cfg.Stores.Official.ShouldContinueIfNewerVersion() {
				officialReviewVersion = version
				logger.Info("official review found; checking whether the selected image version is newer", "package", info.PackageID, "review_version", version)
			} else {
				return pausedOfficialReviewResult(input, operation, version, info, cfg, dependencies, logger, "automatic direct publication paused for official review")
			}
		}
	}
	switch operation {
	case OperationBuild:
		return runBuild(ctx, input, cfg, info, dependencies)
	case OperationCheck:
		return runCheck(ctx, input, cfg, info, officialReviewVersion, dependencies)
	case OperationPublishOfficial:
		return runPublish(ctx, input, operation, cfg, info, dependencies)
	default:
		return Result{}, actionError(CodeConfigInvalid, fmt.Sprintf("unsupported operation %q", operation), nil)
	}
}

func pausedOfficialReviewResult(input Input, operation Operation, reviewVersion string, info project.Info, cfg config.Config, dependencies Dependencies, logger *slog.Logger, message string) (Result, error) {
	if input.Version == "" {
		input.Version = info.Version
	}
	if input.Tag == "" && info.Version != "" {
		input.Tag = "v" + info.Version
	}
	result := baseResult(input, dependencies.Host, info, cfg)
	result.Operation = string(operation)
	result.OfficialReviewPending = true
	result.OfficialReviewVersion = reviewVersion
	logger.Info(message, "package", info.PackageID, "review_version", reviewVersion)
	if err := writeResult(&result, resultDirectory(dependencies.ResultDir, info.Root)); err != nil {
		return Result{}, actionError(CodeStorePublishFailed, "unable to write paused Action result", err)
	}
	return result, nil
}

func shouldPauseForOfficialReview(input Input, requested, resolved Operation, cfg config.Config) bool {
	if input.DryRun || requested != OperationAuto || cfg.Update.Strategy != config.StrategyPublish || !cfg.Stores.Official.Enabled {
		return false
	}
	if input.EventName != "" && input.EventName != "schedule" && input.EventName != "workflow_dispatch" {
		return false
	}
	return resolved == OperationCheck || resolved == OperationBuild
}

func mapWaitingReviewError(err error) *Error {
	if errors.Is(err, lpkgo.ErrUnauthenticated) || errors.Is(err, lpkgo.ErrPermissionDenied) {
		return actionError(CodeStoreAuthFailed, "official waiting-review authentication failed", err)
	}
	return actionError(CodeStorePublishFailed, "unable to query official waiting-review version", err)
}

func executionMode(operation Operation, cfg config.Config) string {
	switch operation {
	case OperationCheck:
		if cfg.Version == 2 {
			return "source-pipeline"
		}
		return "docker-image"
	case OperationPublishOfficial:
		return "store-publish"
	case OperationBuild:
		if cfg.Build.ShouldRunBuildScript() {
			return "source-build"
		}
		return "prebuilt-content"
	default:
		return "unknown"
	}
}

func runPublish(ctx context.Context, input Input, operation Operation, cfg config.Config, info project.Info, dependencies Dependencies) (Result, error) {
	if dependencies.Publish == nil {
		return Result{}, actionError(CodeConfigInvalid, "store publisher dependency is unavailable", nil)
	}
	if input.Version == "" {
		input.Version = info.Version
	}
	if input.Tag == "" && input.Version != "" {
		input.Tag = "v" + input.Version
	}
	if input.Changelog == "" && cfg.Version == 2 && dependencies.ReadState != nil {
		lock, stateErr := dependencies.ReadState(filepath.Join(info.Root, cfg.State.File))
		if stateErr != nil {
			return Result{}, actionError(CodeConfigInvalid, "unable to read packaged changelog", stateErr)
		}
		if lock.Application.Version == input.Version && lock.Status == "packaged" {
			input.Changelog = lock.Application.Changelog
		}
	}
	published, err := dependencies.Publish(ctx, publishflow.Request{
		Config: cfg, Project: info, LPKPath: input.LPKPath, Version: input.Version,
		Changelog: input.Changelog, ExpectedSHA256: input.ExpectedSHA256,
		GuardOfficialReview: input.GuardOfficialReview, DryRun: input.DryRun,
	})
	if err != nil {
		var pendingReview *official.PendingReviewError
		if input.GuardOfficialReview && errors.As(err, &pendingReview) {
			logger := dependencies.Logger
			if logger == nil {
				logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			}
			return pausedOfficialReviewResult(input, operation, pendingReview.Version, info, cfg, dependencies, logger, "official publication paused after final waiting-review recheck")
		}
		return Result{}, mapPublishError(err)
	}
	encodedStores, err := json.Marshal(published)
	if err != nil {
		return Result{}, actionError(CodeStorePublishFailed, "unable to encode store publishing result", err)
	}
	result := baseResult(input, dependencies.Host, info, cfg)
	result.Changelog = input.Changelog
	result.Operation = string(operation)
	result.PackageID = published.Artifact.PackageID
	result.Version = published.Artifact.Version
	result.LPKPath = published.Artifact.Path
	result.SHA256 = published.Artifact.SHA256
	result.TargetPlatform = published.Artifact.TargetPlatform
	result.StoreResults = encodedStores
	if cfg.Version == 2 && !input.DryRun {
		if dependencies.ReadState == nil || dependencies.WriteState == nil {
			return Result{}, actionError(CodeConfigInvalid, "source pipeline state dependencies are unavailable", nil)
		}
		stateFile := filepath.Join(info.Root, cfg.State.File)
		lock, stateErr := dependencies.ReadState(stateFile)
		if stateErr != nil {
			return Result{}, actionError(CodeStorePublishFailed, "unable to read packaged source pipeline state", stateErr)
		}
		if lock.Fingerprint == "" || lock.Status != "packaged" {
			return Result{}, actionError(CodeStorePublishFailed, "official publication requires packaged source pipeline state", nil)
		}
		lock.Status = "submitted"
		lock.Application = pipelinestate.Application{PackageID: published.Artifact.PackageID, Version: published.Artifact.Version, LPKSHA256: published.Artifact.SHA256, Changelog: input.Changelog}
		if stateErr := dependencies.WriteState(stateFile, lock); stateErr != nil {
			return Result{}, actionError(CodeStorePublishFailed, "unable to mark source pipeline state as submitted", stateErr)
		}
		result.StateFile = stateFile
		result.Fingerprint = lock.Fingerprint
		encodedSource, encodeErr := json.Marshal(lock.Source)
		if encodeErr != nil {
			return Result{}, actionError(CodeStorePublishFailed, "unable to encode submitted source pipeline state", encodeErr)
		}
		result.SourceResult = encodedSource
	}
	if err := writeResult(&result, resultDirectory(dependencies.ResultDir, info.Root)); err != nil {
		return Result{}, actionError(CodeStorePublishFailed, "unable to write store publishing result", err)
	}
	return result, nil
}

func runBuild(ctx context.Context, input Input, cfg config.Config, info project.Info, dependencies Dependencies) (Result, error) {
	if input.Version == "" {
		input.Version = info.Version
	}
	if input.Version == "" {
		return Result{}, actionError(CodeVersionNotFound, "a SemVer version is required for build", nil)
	}
	if input.Tag == "" {
		input.Tag = "v" + input.Version
	}
	result := baseResult(input, dependencies.Host, info, cfg)
	result.Operation = string(OperationBuild)
	result.Changed = info.Version != input.Version
	if input.DryRun {
		if err := writeResult(&result, resultDirectory(dependencies.ResultDir, info.Root)); err != nil {
			return Result{}, actionError(CodeBuildFailed, "unable to write dry-run result", err)
		}
		return result, nil
	}

	change, err := dependencies.SetVersion(info.PackageFile, input.Version)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to update package.yml version", err)
	}
	result.Changed = change.Changed
	updated, err := dependencies.Inspect(ctx, cfg.Project)
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeConfigInvalid, "unable to inspect updated LazyCat project", err)
	}
	built, err := dependencies.Build(ctx, actionbuild.Request{
		Project:         updated,
		Version:         input.Version,
		Tag:             input.Tag,
		Channel:         input.Channel,
		SourceDateEpoch: input.SourceDateEpoch,
		Official:        cfg.Stores.Official.Enabled,
		RunBuildScript:  cfg.Build.ShouldRunBuildScript(),
		Target:          cfg.Project.Target(),
	})
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeBuildFailed, "LPK build failed", err)
	}
	if built.TargetPlatform != cfg.Project.Target().Platform() {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeLPKInvalid, fmt.Sprintf("LPK target platform %q does not match required %q", built.TargetPlatform, cfg.Project.Target().Platform()), nil)
	}
	result.PackageID = built.PackageID
	result.LPKPath = built.Path
	result.SHA256 = built.SHA256
	result.TargetPlatform = built.TargetPlatform
	result.Warnings = built.Warnings
	if err := writeResult(&result, resultDirectory(dependencies.ResultDir, updated.Root)); err != nil {
		return Result{}, actionError(CodeBuildFailed, "unable to write Action result", err)
	}
	return result, nil
}

func runCheck(ctx context.Context, input Input, cfg config.Config, info project.Info, officialReviewVersion string, dependencies Dependencies) (Result, error) {
	if cfg.Version == 2 {
		return runSourceCheck(ctx, input, cfg, info, officialReviewVersion, dependencies)
	}
	if cfg.Update.VersionSource.Type != config.VersionSourceImage {
		return Result{}, actionError(CodeConfigInvalid, "check operation requires update.version_source.type=image", nil)
	}
	if cfg.Update.Strategy == config.StrategyPublish && input.ImageID != "" && input.ImageID != cfg.Update.VersionSource.Image {
		return Result{}, actionError(CodeConfigInvalid, fmt.Sprintf("publish strategy image-id %q must select version-source image %q", input.ImageID, cfg.Update.VersionSource.Image), nil)
	}
	if input.EventName == "release" || input.RefType == "tag" {
		return Result{}, actionError(CodeConfigInvalid, "check operation is not supported for tag or release events", nil)
	}
	if dependencies.CheckImages == nil {
		return Result{}, actionError(CodeConfigInvalid, "image check dependency is unavailable", nil)
	}
	checked, err := dependencies.CheckImages(ctx, imageflow.Request{
		Config: cfg, Project: info, ImageID: input.ImageID, DryRun: input.DryRun, OfficialReviewVersion: officialReviewVersion,
	})
	if err != nil {
		if officialReviewVersion != "" && errors.Is(err, imageflow.ErrOfficialReviewCoversCandidate) {
			logger := dependencies.Logger
			if logger == nil {
				logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			}
			return pausedOfficialReviewResult(input, OperationCheck, officialReviewVersion, info, cfg, dependencies, logger, "automatic direct publication paused because official review covers the selected candidate")
		}
		return Result{}, mapImageError(err, cfg.Project.Target())
	}
	input.Version = checked.Version
	input.Tag = "v" + checked.Version
	input.Channel = checked.Channel
	result := baseResult(input, dependencies.Host, info, cfg)
	result.Operation = string(OperationCheck)
	result.Channel = checked.Channel
	encodedImages, err := json.Marshal(checked.Images)
	if err != nil {
		return Result{}, actionError(CodeBuildFailed, "unable to encode image results", err)
	}
	result.ImageResults = encodedImages
	result.Changed = checked.Changed || info.Version != checked.Version
	if input.DryRun || !result.Changed {
		if err := writeResult(&result, resultDirectory(dependencies.ResultDir, info.Root)); err != nil {
			return Result{}, actionError(CodeBuildFailed, "unable to write image check result", err)
		}
		return result, nil
	}

	change, err := dependencies.SetVersion(info.PackageFile, checked.Version)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to update package.yml version", err)
	}
	updated, err := dependencies.Inspect(ctx, cfg.Project)
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeConfigInvalid, "unable to inspect image-updated LazyCat project", err)
	}
	built, err := dependencies.Build(ctx, actionbuild.Request{
		Project: updated, Version: checked.Version, Tag: input.Tag, Channel: checked.Channel,
		SourceDateEpoch: input.SourceDateEpoch, Official: cfg.Stores.Official.Enabled,
		RunBuildScript: cfg.Build.ShouldRunBuildScript(),
		Target:         cfg.Project.Target(),
	})
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeBuildFailed, "LPK validation build failed after image update", err)
	}
	if built.TargetPlatform != cfg.Project.Target().Platform() {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeLPKInvalid, fmt.Sprintf("LPK target platform %q does not match required %q", built.TargetPlatform, cfg.Project.Target().Platform()), nil)
	}
	result.PackageID = built.PackageID
	result.LPKPath = built.Path
	result.SHA256 = built.SHA256
	result.Warnings = built.Warnings
	if err := writeResult(&result, resultDirectory(dependencies.ResultDir, updated.Root)); err != nil {
		return Result{}, actionError(CodeBuildFailed, "unable to write image update result", err)
	}
	return result, nil
}

func runSourceCheck(ctx context.Context, input Input, cfg config.Config, info project.Info, officialReviewVersion string, dependencies Dependencies) (Result, error) {
	if officialReviewVersion != "" {
		logger := dependencies.Logger
		if logger == nil {
			logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		}
		return pausedOfficialReviewResult(input, OperationCheck, officialReviewVersion, info, cfg, dependencies, logger, "automatic source publication paused while an official review is pending")
	}
	if dependencies.DiscoverSource == nil || dependencies.PrepareSource == nil || dependencies.ReadState == nil || dependencies.WriteState == nil || dependencies.Fingerprint == nil {
		return Result{}, actionError(CodeConfigInvalid, "source pipeline dependencies are unavailable", nil)
	}
	candidate, err := dependencies.DiscoverSource(ctx, source.Request{Source: cfg.Source, Target: cfg.Project.Target(), CurrentVersion: info.Version})
	if err != nil {
		return Result{}, actionError(CodeVersionNotFound, "unable to discover the configured source", err)
	}
	fingerprint, err := dependencies.Fingerprint(ctx, cfg, candidate)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to fingerprint source pipeline inputs", err)
	}
	stateFile := filepath.Join(info.Root, cfg.State.File)
	lock, err := dependencies.ReadState(stateFile)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to read source pipeline state", err)
	}
	terminalStatus := "submitted"
	if cfg.Update.Strategy == config.StrategyPull {
		terminalStatus = "packaged"
	}
	changed := lock.Fingerprint != fingerprint || lock.Status != terminalStatus
	version, err := sourceApplicationVersion(candidate.Version, info.Version, lock, fingerprint)
	if err != nil {
		return Result{}, actionError(CodeVersionNotFound, "unable to derive the next application version", err)
	}
	encodedSource, err := json.Marshal(candidate)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to encode source discovery result", err)
	}
	input.Version = version
	input.Tag = "v" + version
	result := baseResult(input, dependencies.Host, info, cfg)
	result.Operation = string(OperationCheck)
	result.Changed = changed
	result.SourceResult = encodedSource
	result.Fingerprint = fingerprint
	result.StateFile = stateFile
	if changed {
		if input.Changelog != "" {
			result.Changelog = input.Changelog
		} else if lock.Fingerprint == fingerprint && lock.Application.Changelog != "" {
			result.Changelog = lock.Application.Changelog
		} else if cfg.Changelog.GitURL != "" {
			if dependencies.DiscoverChangelog == nil {
				return Result{}, actionError(CodeConfigInvalid, "changelog discovery is unavailable", nil)
			}
			result.Changelog, err = dependencies.DiscoverChangelog(ctx, cfg.Changelog, candidate, lock.Source, info.Version, version)
			if err != nil {
				return Result{}, actionError(CodeVersionNotFound, "unable to discover the upstream changelog", err)
			}
		}
	}
	if !changed || input.DryRun {
		if err := writeResult(&result, resultDirectory(dependencies.ResultDir, info.Root)); err != nil {
			return Result{}, actionError(CodeBuildFailed, "unable to write source discovery result", err)
		}
		return result, nil
	}

	prepared, err := dependencies.PrepareSource(ctx, prepare.Request{
		ProjectRoot: info.Root, Config: cfg, Candidate: candidate, ApplicationVersion: version, Fingerprint: fingerprint,
		Target: cfg.Project.Target(), Stdout: os.Stdout, Stderr: os.Stderr,
	})
	if err != nil {
		return Result{}, actionError(CodeBuildFailed, "source preparation failed", err)
	}
	imageResults := []map[string]any{}
	if strings.TrimSpace(prepared.Image) != "" {
		if len(cfg.Images) != 1 {
			return Result{}, actionError(CodeConfigInvalid, "prepared image requires exactly one runtime image binding", nil)
		}
		if dependencies.InspectPreparedImage == nil || dependencies.DeliverPreparedImage == nil || dependencies.ReadManifestImages == nil || dependencies.ApplyManifestImages == nil {
			return Result{}, actionError(CodeConfigInvalid, "prepared image delivery dependencies are unavailable", nil)
		}
		binding := cfg.Images[0]
		target := manifestTarget(binding)
		current, err := dependencies.ReadManifestImages(info.ManifestFile, []manifestedit.Target{target})
		if err != nil || len(current) != 1 {
			return Result{}, actionError(CodeConfigInvalid, "unable to read prepared image target", err)
		}
		inspected, err := dependencies.InspectPreparedImage(ctx, prepared.Image, cfg.Project.Target())
		if err != nil {
			return Result{}, actionError(CodePlatformNotFound, "unable to inspect prepared image for the target platform", err)
		}
		sourceReference := prepared.Image
		if !strings.Contains(sourceReference, "@") {
			sourceReference += "@" + strings.TrimPrefix(inspected.Digest, "@")
		}
		delivered, err := dependencies.DeliverPreparedImage(ctx, delivery.Request{
			Image: binding, Tag: candidate.Tag, SourceRef: sourceReference, SourceDigest: inspected.Digest,
			CurrentRef: current[0].RuntimeRef, Target: cfg.Project.Target(),
		})
		if err != nil {
			return Result{}, actionError(CodeImageCopyFailed, "prepared image delivery failed", err)
		}
		if strings.TrimSpace(delivered.RuntimeRef) == "" {
			return Result{}, actionError(CodeImageCopyFailed, "prepared image delivery returned an empty runtime image", nil)
		}
		if _, err := dependencies.ApplyManifestImages(info.ManifestFile, []manifestedit.Update{{
			Target: target, SourceRef: sourceReference, RuntimeRef: delivered.RuntimeRef,
		}}); err != nil {
			return Result{}, actionError(CodeConfigInvalid, "unable to update prepared image in the LazyCat Manifest", err)
		}
		imageResults = append(imageResults, map[string]any{
			"sourceRef": sourceReference, "sourceDigest": inspected.Digest,
			"deliveredRef": delivered.RuntimeRef, "copied": delivered.Copied,
		})
	} else if len(cfg.Images) != 0 {
		return Result{}, actionError(CodeConfigInvalid, "runtime image binding requires build.prepare.output_image", nil)
	}
	encodedImages, err := json.Marshal(imageResults)
	if err != nil {
		return Result{}, actionError(CodeBuildFailed, "unable to encode prepared image result", err)
	}
	result.ImageResults = encodedImages
	change, err := dependencies.SetVersion(info.PackageFile, version)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to update package.yml version", err)
	}
	updated, err := dependencies.Inspect(ctx, cfg.Project)
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeConfigInvalid, "unable to inspect source-updated LazyCat project", err)
	}
	built, err := dependencies.Build(ctx, actionbuild.Request{
		Project: updated, Version: version, Tag: input.Tag, SourceDateEpoch: input.SourceDateEpoch,
		Official: cfg.Stores.Official.Enabled, RunBuildScript: cfg.Build.ShouldRunBuildScript(), Target: cfg.Project.Target(),
	})
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeBuildFailed, "LPK validation build failed after source update", err)
	}
	lock = pipelinestate.Lock{
		Source: candidate, Fingerprint: fingerprint, Status: "packaged",
		Application: pipelinestate.Application{PackageID: built.PackageID, Version: built.Version, LPKSHA256: built.SHA256, Changelog: result.Changelog},
	}
	if err := dependencies.WriteState(stateFile, lock); err != nil {
		return Result{}, actionError(CodeBuildFailed, "unable to write packaged source pipeline state", err)
	}
	result.PackageID = built.PackageID
	result.LPKPath = built.Path
	result.SHA256 = built.SHA256
	result.TargetPlatform = built.TargetPlatform
	result.Warnings = built.Warnings
	if err := writeResult(&result, resultDirectory(dependencies.ResultDir, updated.Root)); err != nil {
		return Result{}, actionError(CodeBuildFailed, "unable to write source pipeline result", err)
	}
	return result, nil
}

func sourceApplicationVersion(candidateVersion, currentVersion string, lock pipelinestate.Lock, fingerprint string) (string, error) {
	if lock.Fingerprint == fingerprint && (lock.Status == "packaged" || lock.Status == "submitted") && strings.TrimSpace(lock.Application.Version) != "" {
		return lock.Application.Version, nil
	}
	candidateVersion = strings.TrimSpace(candidateVersion)
	if candidateVersion == "" {
		return versioning.BumpPatch(currentVersion)
	}
	candidate, candidateErr := semver.StrictNewVersion(candidateVersion)
	current, currentErr := semver.StrictNewVersion(strings.TrimSpace(currentVersion))
	if candidateErr != nil || currentErr != nil {
		return "", errors.Join(candidateErr, currentErr)
	}
	if candidate.GreaterThan(current) {
		return candidate.String(), nil
	}
	return versioning.BumpPatch(currentVersion)
}

func manifestTarget(image config.Image) manifestedit.Target {
	target := manifestedit.Target{ID: image.ID, Service: image.Service}
	if image.Target == "application" {
		target.Kind = manifestedit.TargetApplication
	} else {
		target.Kind = manifestedit.TargetService
	}
	return target
}

func baseResult(input Input, host platform.Host, info project.Info, cfg config.Config) Result {
	return Result{
		PackageID:            info.PackageID,
		PackageFile:          info.PackageFile,
		ManifestFile:         info.ManifestFile,
		Version:              input.Version,
		Tag:                  input.Tag,
		ImageResults:         json.RawMessage("[]"),
		SourceResult:         json.RawMessage("{}"),
		StoreResults:         json.RawMessage("{}"),
		UpdateStrategy:       string(cfg.Update.Strategy),
		OfficialStoreEnabled: cfg.Stores.Official.Enabled,
		Channel:              input.Channel,
		RunnerArch:           host.Arch,
		TargetPlatform:       cfg.Project.Target().Platform(),
	}
}

func mapImageError(err error, target platform.Target) *Error {
	switch {
	case errors.Is(err, imageflow.ErrVersionDowngrade):
		return actionError(CodeVersionDowngradeBlocked, "selected image version is lower than the current application version; set update.allow_downgrade=true only for an intentional rollback", err)
	case errors.Is(err, imageflow.ErrVersionNotFound):
		return actionError(CodeVersionNotFound, "no image version matched the configured channel", err)
	case errors.Is(err, imageflow.ErrPlatformNotFound):
		return actionError(CodePlatformNotFound, fmt.Sprintf("the configured image has no usable %s candidate", target.Platform()), err)
	case errors.Is(err, imageflow.ErrMirrorVerificationFailed):
		return actionError(CodeMirrorVerificationFailed, "mirror image verification failed", err)
	case errors.Is(err, imageflow.ErrDeliveryFailed):
		return actionError(CodeImageCopyFailed, "image delivery failed", err)
	default:
		return actionError(CodeConfigInvalid, "image check failed", err)
	}
}

func mapPublishError(err error) *Error {
	switch {
	case errors.Is(err, publishflow.ErrPublishStrategyRequired), errors.Is(err, publishflow.ErrStoreDisabled), errors.Is(err, lpkgo.ErrInvalidArgument), errors.Is(err, lpkgo.ErrInvalidConfig):
		return actionError(CodeConfigInvalid, "store publishing configuration is invalid", err)
	case errors.Is(err, lpkgo.ErrUnauthenticated), errors.Is(err, lpkgo.ErrPermissionDenied):
		return actionError(CodeStoreAuthFailed, "store authentication failed", err)
	case errors.Is(err, lpkgo.ErrInvalidManifest), errors.Is(err, lpkgo.ErrUnsupportedFormat), errors.Is(err, lpkgo.ErrIntegrityMismatch):
		return actionError(CodeLPKInvalid, "LPK validation failed before store publishing", err)
	default:
		return actionError(CodeStorePublishFailed, "store publishing failed", err)
	}
}

func validateDependencies(dependencies Dependencies) error {
	if dependencies.Host.OS == "" || dependencies.Host.Arch == "" || dependencies.LoadConfig == nil || dependencies.Inspect == nil || dependencies.SetVersion == nil || dependencies.Build == nil {
		return errors.New("host, loader, inspector, editor, and builder are required")
	}
	return nil
}

func validateWorkflowToolchains(input Input, configured []config.Toolchain) error {
	declared := strings.TrimSpace(input.WorkflowToolchains)
	if declared == "" || len(configured) == 0 {
		return nil
	}
	want := make(map[string]string, len(configured))
	for _, toolchain := range configured {
		want[toolchain.Kind] = toolchain.Version
	}
	got := make(map[string]struct{})
	if declared != "none" {
		for _, kind := range strings.Split(declared, ",") {
			got[strings.TrimSpace(kind)] = struct{}{}
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("workflow declares %q; config declares %v", declared, sortedToolchainKinds(want))
	}
	for kind := range want {
		if _, found := got[kind]; !found {
			return fmt.Errorf("workflow declares %q; config declares %v", declared, sortedToolchainKinds(want))
		}
	}
	explicitVersions := map[string]string{
		"go": input.WorkflowGoVersion, "node": input.WorkflowNodeVersion, "rust": input.WorkflowRustToolchain,
	}
	for kind, configuredVersion := range want {
		workflowVersion := strings.TrimSpace(explicitVersions[kind])
		if configuredVersion != "" && workflowVersion != "" && configuredVersion != workflowVersion {
			return fmt.Errorf("%s version %q does not match configured version %q", kind, workflowVersion, configuredVersion)
		}
	}
	return nil
}

func sortedToolchainKinds(toolchains map[string]string) []string {
	kinds := make([]string, 0, len(toolchains))
	for kind := range toolchains {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

func rollbackVersion(dependencies Dependencies, filename string, change yamledit.Change) {
	if change.Changed && change.Old != "" {
		_, _ = dependencies.SetVersion(filename, change.Old)
	}
}

func resultDirectory(configured, root string) string {
	if configured != "" {
		return configured
	}
	return filepath.Join(root, ".lazycat-action")
}

func writeResult(result *Result, directory string) (resultErr error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("result directory %q must not be a symbolic link", absolute)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return err
	}
	result.ResultFile = filepath.Join(absolute, "result.json")
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(absolute, ".result-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, result.ResultFile)
}

func actionError(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}
