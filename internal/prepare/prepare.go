package prepare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/source"
)

type Request struct {
	ProjectRoot        string
	Config             config.Config
	Candidate          source.Candidate
	ApplicationVersion string
	Fingerprint        string
	Target             platform.Target
	Stdout             io.Writer
	Stderr             io.Writer
}

type Result struct {
	Image     string `json:"image,omitempty"`
	SourceDir string `json:"sourceDir,omitempty"`
}

type Runner struct {
	Git source.GitRunner
	Run func(context.Context, string, []string, []string, io.Writer, io.Writer) error
}

func (runner Runner) Prepare(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("prepare context is required")
	}
	root, err := filepath.Abs(request.ProjectRoot)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project root: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(request.Config.Build.Prepare.Mode))
	if mode == "" {
		mode = "passthrough"
	}
	workDirectory, err := os.MkdirTemp("", "lazycat-source-")
	if err != nil {
		return Result{}, fmt.Errorf("create source workspace: %w", err)
	}
	defer os.RemoveAll(workDirectory)
	sourceDirectory := ""
	if request.Config.Source.Kind == config.SourceKindGit && mode != "passthrough" {
		sourceDirectory = filepath.Join(workDirectory, "source")
		if err := runner.Git.Checkout(ctx, request.Config.Source, request.Candidate, sourceDirectory); err != nil {
			return Result{}, err
		}
	}
	outputImage, err := expandOutput(request.Config.Build.Prepare.Output, request.Candidate, request.ApplicationVersion, request.Fingerprint)
	if err != nil {
		return Result{}, err
	}
	environment := []string{
		"LAZYCAT_SOURCE_KIND=" + request.Candidate.Kind,
		"LAZYCAT_SOURCE_REF=" + request.Candidate.Ref,
		"LAZYCAT_SOURCE_REVISION=" + request.Candidate.Revision,
		"LAZYCAT_SOURCE_VERSION=" + request.Candidate.Version,
		"LAZYCAT_APPLICATION_VERSION=" + request.ApplicationVersion,
		"LAZYCAT_SOURCE_DIR=" + sourceDirectory,
		"LAZYCAT_BUILD_FINGERPRINT=" + request.Fingerprint,
		"LAZYCAT_OUTPUT_IMAGE=" + outputImage,
		"LAZYCAT_TARGET_PLATFORM=" + request.Target.Platform(),
	}
	switch mode {
	case "passthrough":
		if request.Candidate.Kind != string(config.SourceKindOCI) {
			return Result{}, errors.New("passthrough build mode requires an OCI source")
		}
		image := request.Candidate.Ref
		if request.Candidate.Revision != "" {
			image += "@" + request.Candidate.Revision
		}
		return Result{Image: image}, nil
	case "command":
		command := strings.TrimSpace(request.Config.Build.Prepare.Command)
		if command == "" {
			return Result{}, errors.New("command build mode requires build.prepare.command")
		}
		if err := runner.run(ctx, root, []string{"/bin/sh", "-eu", "-c", command}, environment, request.Stdout, request.Stderr); err != nil {
			return Result{}, fmt.Errorf("run build prepare command: %w", err)
		}
		return Result{Image: outputImage, SourceDir: sourceDirectory}, nil
	case "dockerfile":
		if outputImage == "" {
			return Result{}, errors.New("dockerfile build mode requires build.prepare.output_image")
		}
		dockerfile, err := beneath(root, request.Config.Build.Prepare.Dockerfile)
		if err != nil {
			return Result{}, fmt.Errorf("resolve prepare Dockerfile: %w", err)
		}
		contextDirectory := sourceDirectory
		configuredContext := strings.TrimSpace(request.Config.Build.Prepare.Context)
		if configuredContext != "" && configuredContext != "source" {
			contextDirectory, err = beneath(root, configuredContext)
			if err != nil {
				return Result{}, fmt.Errorf("resolve prepare context: %w", err)
			}
		}
		if contextDirectory == "" {
			contextDirectory = root
		}
		arguments := []string{
			"buildx", "build", "--platform", request.Target.Platform(),
			"--file", dockerfile, "--tag", outputImage, "--push",
			"--build-arg", "SOURCE_REF=" + request.Candidate.Ref,
			"--build-arg", "SOURCE_REVISION=" + request.Candidate.Revision,
			"--build-arg", "SOURCE_VERSION=" + request.Candidate.Version,
			"--build-arg", "APPLICATION_VERSION=" + request.ApplicationVersion,
		}
		if request.Candidate.Kind == string(config.SourceKindOCI) {
			sourceImage := request.Candidate.Ref
			if request.Candidate.Revision != "" {
				sourceImage += "@" + request.Candidate.Revision
			}
			arguments = append(arguments, "--build-arg", "SOURCE_IMAGE="+sourceImage)
		}
		keys := make([]string, 0, len(request.Config.Build.Prepare.BuildArgs))
		for key := range request.Config.Build.Prepare.BuildArgs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			arguments = append(arguments, "--build-arg", key+"="+request.Config.Build.Prepare.BuildArgs[key])
		}
		arguments = append(arguments, contextDirectory)
		if err := runner.run(ctx, root, append([]string{"docker"}, arguments...), environment, request.Stdout, request.Stderr); err != nil {
			return Result{}, fmt.Errorf("build and push prepared image: %w", err)
		}
		return Result{Image: outputImage, SourceDir: sourceDirectory}, nil
	default:
		return Result{}, fmt.Errorf("unsupported build prepare mode %q", mode)
	}
}

func (runner Runner) run(ctx context.Context, directory string, command, environment []string, stdout, stderr io.Writer) error {
	if runner.Run != nil {
		return runner.Run(ctx, directory, command, environment, stdout, stderr)
	}
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Dir = directory
	process.Env = append(safeEnvironment(os.Environ()), environment...)
	process.Stdout = stdout
	process.Stderr = stderr
	if err := process.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return fmt.Errorf("process exited with status %d", exitError.ExitCode())
		}
		return err
	}
	return nil
}

func safeEnvironment(environment []string) []string {
	blocked := map[string]struct{}{
		"LZC_API_TOKEN": {}, "LZC_API_HOST": {}, "LZC_CLI_TOKEN": {},
		"LAZYCAT_TOKEN": {}, "LAZYCAT_USERNAME": {}, "LAZYCAT_PASSWORD": {},
		"LZC_APPSTORE_COS_DOMAIN": {},
		"APPSTORE_TOKEN":          {}, "APPSTORE_URL": {}, "APP_ID": {}, "PRIVATE_STORE_GROUP_CODES": {},
		"GITHUB_TOKEN": {}, "GH_TOKEN": {}, "REGISTRY_USERNAME": {}, "REGISTRY_PASSWORD": {},
		"LAZYCAT_GIT_TOKEN": {}, "LAZYCAT_GIT_USERNAME": {},
		"GITHUB_OUTPUT": {}, "GITHUB_ENV": {}, "GITHUB_STATE": {}, "GITHUB_STEP_SUMMARY": {},
		"ACTIONS_RUNTIME_TOKEN": {}, "ACTIONS_ID_TOKEN_REQUEST_TOKEN": {}, "ACTIONS_RESULTS_URL": {},
	}
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, denied := blocked[key]; denied || strings.HasPrefix(key, "LAZYCAT_AUTH_") {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func expandOutput(template string, candidate source.Candidate, applicationVersion, fingerprint string) (string, error) {
	template = strings.TrimSpace(template)
	if template == "" {
		return "", nil
	}
	shortRevision := candidate.Revision
	shortRevision = strings.TrimPrefix(shortRevision, "sha256:")
	if len(shortRevision) > 12 {
		shortRevision = shortRevision[:12]
	}
	shortFingerprint := strings.TrimPrefix(fingerprint, "sha256:")
	if len(shortFingerprint) > 12 {
		shortFingerprint = shortFingerprint[:12]
	}
	replacements := map[string]string{
		"{version}": applicationVersion, "{source_version}": candidate.Version,
		"{revision}": shortRevision, "{fingerprint}": shortFingerprint,
	}
	result := template
	for placeholder, value := range replacements {
		result = strings.ReplaceAll(result, placeholder, value)
	}
	if strings.Contains(result, "{") || strings.Contains(result, "}") {
		return "", fmt.Errorf("output image template %q contains an unsupported placeholder", template)
	}
	return result, nil
}

func beneath(root, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(name) {
		return "", errors.New("path must be relative to project root")
	}
	path := filepath.Clean(filepath.Join(root, name))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes project root")
	}
	return path, nil
}
