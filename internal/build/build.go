package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/diagnostic"
	"github.com/wcaqrl/lazycat-action/internal/lpkcheck"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/project"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/lint"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

type Request struct {
	Project         project.Info
	Version         string
	Tag             string
	Channel         string
	SourceDateEpoch int64
	Official        bool
	FailOnWarnings  bool
	RunBuildScript  bool
	Target          platform.Target
	Runner          toolkitbuild.CommandRunner
}

type Result struct {
	Path           string          `json:"path"`
	PackageID      string          `json:"packageId"`
	Version        string          `json:"version"`
	SHA256         string          `json:"sha256"`
	Size           int64           `json:"size"`
	TargetPlatform string          `json:"targetPlatform"`
	Warnings       []lpkgo.Warning `json:"warnings,omitempty"`
}

type Builder struct {
	Stdout io.Writer
	Stderr io.Writer
	Logger *slog.Logger
}

type publicBuildDiagnostic struct {
	cause  error
	path   string
	detail string
}

func (diagnostic publicBuildDiagnostic) Error() string {
	if diagnostic.detail != "" {
		return diagnostic.detail
	}
	return "local LPK build validation failed"
}

func (diagnostic publicBuildDiagnostic) Unwrap() error {
	return diagnostic.cause
}

func (diagnostic publicBuildDiagnostic) PublicErrorDetail() string {
	return diagnostic.detail
}

func (diagnostic publicBuildDiagnostic) PublicErrorPath() string {
	return diagnostic.path
}

var protectedBuildEnvironment = map[string]struct{}{
	"ACTIONS_CACHE_URL":              {},
	"ACTIONS_ID_TOKEN_REQUEST_TOKEN": {},
	"ACTIONS_ID_TOKEN_REQUEST_URL":   {},
	"ACTIONS_RESULTS_URL":            {},
	"ACTIONS_RUNTIME_TOKEN":          {},
	"GH_TOKEN":                       {},
	"APPSTORE_TOKEN":                 {},
	"APPSTORE_URL":                   {},
	"APP_ID":                         {},
	"PRIVATE_STORE_GROUP_CODES":      {},
	"GITHUB_ENV":                     {},
	"GITHUB_OUTPUT":                  {},
	"GITHUB_PATH":                    {},
	"GITHUB_STATE":                   {},
	"GITHUB_STEP_SUMMARY":            {},
	"GITHUB_TOKEN":                   {},
	"LAZYCAT_PASSWORD":               {},
	"LAZYCAT_TOKEN":                  {},
	"LAZYCAT_USERNAME":               {},
	"LZC_CLI_TOKEN":                  {},
	"LZC_API_HOST":                   {},
	"LZC_APPSTORE_COS_DOMAIN":        {},
	"LZC_API_TOKEN":                  {},
	"INPUT_SHA256":                   {},
	"REGISTRY_PASSWORD":              {},
	"REGISTRY_USERNAME":              {},
}

type protectedRunner struct {
	base toolkitbuild.CommandRunner
}

func (runner protectedRunner) Run(ctx context.Context, command toolkitbuild.Command) error {
	environment := make(map[string]string, len(command.Env))
	for key, value := range command.Env {
		if _, protected := protectedBuildEnvironment[key]; protected {
			continue
		}
		environment[key] = value
	}
	command.Env = environment
	return runner.base.Run(ctx, command)
}

func (builder Builder) Build(ctx context.Context, request Request) (result Result, resultErr error) {
	if ctx == nil {
		return Result{}, errors.New("build LPK: nil context")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("build LPK: %w", err)
	}
	if strings.TrimSpace(request.Project.Root) == "" || strings.TrimSpace(request.Project.Output) == "" {
		return Result{}, errors.New("build LPK: project root and output are required")
	}
	if request.Version == "" {
		return Result{}, errors.New("build LPK: version is required")
	}
	if request.Project.Version != request.Version {
		return Result{}, fmt.Errorf("build LPK: package version %q does not match requested version %q", request.Project.Version, request.Version)
	}
	if request.Tag == "" {
		request.Tag = "v" + request.Version
	}
	target, err := request.Target.Normalize()
	if err != nil {
		return Result{}, fmt.Errorf("build LPK: %w", err)
	}
	builder.logger().Info("LPK build started", "package", request.Project.PackageID, "version", request.Version, "target", target.Platform())

	output := filepath.Clean(request.Project.Output)
	outputDirectory := filepath.Dir(output)
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return Result{}, fmt.Errorf("create LPK output directory %q: %w", outputDirectory, err)
	}
	reserved, err := os.CreateTemp(outputDirectory, ".lazycat-action-*.lpk")
	if err != nil {
		return Result{}, fmt.Errorf("reserve temporary LPK in %q: %w", outputDirectory, err)
	}
	temporary := reserved.Name()
	if closeErr := reserved.Close(); closeErr != nil {
		_ = os.Remove(temporary)
		return Result{}, fmt.Errorf("close reserved LPK %q: %w", temporary, closeErr)
	}
	if err := os.Remove(temporary); err != nil {
		return Result{}, fmt.Errorf("prepare temporary LPK %q: %w", temporary, err)
	}
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporary)
		}
	}()

	environment := map[string]string{
		"LAZYCAT_VERSION":         request.Version,
		"LAZYCAT_TAG":             request.Tag,
		"LAZYCAT_CHANNEL":         request.Channel,
		"LAZYCAT_TARGET_OS":       target.OS,
		"LAZYCAT_TARGET_ARCH":     target.Arch,
		"LAZYCAT_TARGET_PLATFORM": target.Platform(),
		"SOURCE_DATE_EPOCH":       strconv.FormatInt(request.SourceDateEpoch, 10),
	}
	runner := request.Runner
	if runner == nil {
		runner = streamingShellRunner{stdout: builder.stdout(), stderr: builder.stderr()}
	}
	if request.RunBuildScript {
		builder.logger().Info("project buildscript started")
	} else {
		builder.logger().Info("project buildscript disabled")
	}
	toolkitResult, err := toolkitbuild.BuildFile(ctx, temporary, toolkitbuild.Request{
		Root:               request.Project.Root,
		ConfigFile:         request.Project.BuildConfig,
		Environment:        environment,
		InheritEnvironment: true,
		RunBuildScript:     request.RunBuildScript,
		Runner:             protectedRunner{base: runner},
	})
	if err != nil {
		return Result{}, wrapToolkitBuildError(request.Project, err)
	}
	builder.logger().Info("LPK package assembled; verifying metadata and contents")

	reader, err := lpk.OpenFile(ctx, temporary)
	if err != nil {
		return Result{}, fmt.Errorf("reopen built LPK: %w", err)
	}
	packageDocument, packageErr := reader.PackageInfo(ctx)
	if packageErr != nil {
		_ = reader.Close()
		return Result{}, fmt.Errorf("read built LPK package metadata: %w", packageErr)
	}
	var packageInfo manifest.PackageInfo
	if err := packageDocument.Decode(&packageInfo); err != nil {
		_ = reader.Close()
		return Result{}, fmt.Errorf("decode built LPK package metadata: %w", err)
	}
	if packageInfo.Package != request.Project.PackageID {
		_ = reader.Close()
		return Result{}, fmt.Errorf("verify built LPK: package %q does not match expected %q", packageInfo.Package, request.Project.PackageID)
	}
	if packageInfo.Version != request.Version {
		_ = reader.Close()
		return Result{}, fmt.Errorf("verify built LPK: version %q does not match expected %q", packageInfo.Version, request.Version)
	}

	extractionParent, err := os.MkdirTemp("", "lazycat-action-lint-*")
	if err != nil {
		_ = reader.Close()
		return Result{}, fmt.Errorf("create LPK lint directory: %w", err)
	}
	defer os.RemoveAll(extractionParent)
	extractionRoot := filepath.Join(extractionParent, "root")
	extractErr := reader.Extract(ctx, extractionRoot)
	closeErr := reader.Close()
	if extractErr != nil || closeErr != nil {
		return Result{}, fmt.Errorf("extract built LPK for lint: %w", errors.Join(extractErr, closeErr))
	}
	var lintOptions []lint.Option
	if request.Official {
		builder.logger().Info("official-store lint started")
		lintOptions = append(lintOptions, lint.WithOfficial())
	}
	lintWarnings, err := lint.Package(ctx, os.DirFS(extractionRoot), lintOptions...)
	if err != nil {
		return Result{}, fmt.Errorf("lint built LPK: %w", err)
	}
	warnings := append([]lpkgo.Warning(nil), toolkitResult.Warnings...)
	warnings = append(warnings, lintWarnings...)
	blockingWarnings := lintWarnings
	if request.Official {
		blockingWarnings = make([]lpkgo.Warning, 0, len(lintWarnings))
		for _, warning := range lintWarnings {
			if lint.IsOfficialWarning(warning) {
				blockingWarnings = append(blockingWarnings, warning)
			}
		}
	}
	if request.FailOnWarnings && len(blockingWarnings) > 0 {
		profile := "basic lint"
		if request.Official {
			profile = "official lint"
		}
		return Result{}, fmt.Errorf("%s reported %d warning(s)", profile, len(blockingWarnings))
	}

	digest, size, err := lpkcheck.HashFile(ctx, temporary)
	if err != nil {
		return Result{}, err
	}
	if err := os.Rename(temporary, output); err != nil {
		return Result{}, fmt.Errorf("publish LPK %q: %w", output, err)
	}
	if err := syncDirectory(outputDirectory); err != nil {
		return Result{}, err
	}
	result = Result{
		Path:           output,
		PackageID:      packageInfo.Package,
		Version:        packageInfo.Version,
		SHA256:         digest,
		Size:           size,
		TargetPlatform: target.Platform(),
		Warnings:       warnings,
	}
	builder.logger().Info("LPK build completed", "path", result.Path, "size_bytes", result.Size, "sha256", result.SHA256)
	return result, nil
}

func (builder Builder) stdout() io.Writer {
	if builder.Stdout != nil {
		return builder.Stdout
	}
	return os.Stdout
}

func (builder Builder) stderr() io.Writer {
	if builder.Stderr != nil {
		return builder.Stderr
	}
	return os.Stderr
}

func (builder Builder) logger() *slog.Logger {
	if builder.Logger != nil {
		return builder.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func wrapToolkitBuildError(info project.Info, err error) error {
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) {
		return fmt.Errorf("build LPK with toolkit: %w", err)
	}
	clone := *toolkitError
	clone.Path = diagnostic.KnownProjectPath(info.Root, toolkitError.Path, info.BuildConfig, info.ManifestFile, info.PackageFile)
	publicDiagnostic := publicBuildDiagnostic{
		cause:  toolkitError.Cause,
		path:   clone.Path,
		detail: safePublicBuildDetail(toolkitError),
	}
	clone.Cause = publicDiagnostic
	if publicDiagnostic.detail != "" {
		return fmt.Errorf("build LPK with toolkit: %s: %w", publicDiagnostic.detail, &clone)
	}
	return fmt.Errorf("build LPK with toolkit: %w", &clone)
}

func safePublicBuildDetail(toolkitError *lpkgo.Error) string {
	if toolkitError == nil || toolkitError.Cause == nil {
		return ""
	}
	if toolkitError.Code == lpkgo.CodeInvalidConfig || toolkitError.Code == lpkgo.CodeInvalidManifest {
		return diagnostic.SafeYAMLSyntaxDetail(toolkitError.Cause)
	}
	if toolkitError.Code == lpkgo.CodeCommandFailed {
		return safeBuildscriptExitDetail(toolkitError.Cause)
	}
	return ""
}

func safeBuildscriptExitDetail(err error) string {
	const prefix = "buildscript exited with code "
	detail := strings.Join(strings.Fields(strings.ToValidUTF8(err.Error(), "")), " ")
	if !strings.HasPrefix(detail, prefix) {
		return ""
	}
	code := strings.TrimPrefix(detail, prefix)
	if code == "" || code[0] == '0' {
		return ""
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return ""
		}
	}
	return detail
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open LPK output directory for sync: %w", err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("sync LPK output directory: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}
