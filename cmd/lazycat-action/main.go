package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/wcaqrl/lazycat-action/internal/action"
	"github.com/wcaqrl/lazycat-action/internal/githubio"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cliMode := false
	publishAfterCheck := false
	if len(args) > 0 {
		if len(args) == 1 && args[0] == "--version" {
			if err := json.NewEncoder(stdout).Encode(version.Info()); err != nil {
				fmt.Fprintln(stderr, "unable to encode version information")
				return 1
			}
			return 0
		}
		if args[0] != "run" {
			fmt.Fprintln(stderr, "usage: lazycat-action [--version] | run [options]")
			return 2
		}
		cliMode = true
		flags := flag.NewFlagSet("lazycat-action run", flag.ContinueOnError)
		flags.SetOutput(stderr)
		operation := flags.String("operation", "auto", "auto, check, build, or publish-official")
		configPath := flags.String("config", ".github/lazycat-action.yml", "pipeline configuration path")
		imageID := flags.String("image-id", "", "legacy version 1 image ID")
		versionValue := flags.String("version", "", "application SemVer")
		changelog := flags.String("changelog", "", "official review changelog")
		lpkPath := flags.String("lpk-path", "", "existing LPK path")
		sha256 := flags.String("sha256", "", "expected LPK SHA256")
		dryRun := flags.Bool("dry-run", false, "plan without mutating files or remote state")
		publish := flags.Bool("publish-after-check", false, "submit a newly packaged update to the official store")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() != 0 {
			fmt.Fprintln(stderr, "lazycat-action run does not accept positional arguments")
			return 2
		}
		overrides := map[string]string{
			"INPUT_OPERATION": *operation, "INPUT_CONFIG": *configPath, "INPUT_IMAGE_ID": *imageID,
			"INPUT_VERSION": *versionValue, "INPUT_CHANGELOG": *changelog, "INPUT_LPK_PATH": *lpkPath,
			"INPUT_SHA256": *sha256, "INPUT_DRY_RUN": fmt.Sprintf("%t", *dryRun),
		}
		baseGetenv := getenv
		publishAfterCheck = *publish
		getenv = func(name string) string {
			if value, found := overrides[name]; found {
				return value
			}
			return baseGetenv(name)
		}
	}

	host, err := platform.NormalizeHost(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	input, err := githubio.ReadInput(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %s\n", action.CodeConfigInvalid, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	dependencies, err := action.DefaultDependenciesWithEnv(host, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %s\n", action.CodeConfigInvalid, err)
		return 1
	}
	result, err := action.Run(ctx, input, dependencies)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if publishInput, ok := followupPublishInput(publishAfterCheck, input, result); ok {
		published, publishErr := action.Run(ctx, publishInput, dependencies)
		if publishErr != nil {
			fmt.Fprintln(stderr, publishErr)
			return 1
		}
		published.Changed = result.Changed
		published.ImageResults = result.ImageResults
		if len(published.SourceResult) == 0 || string(published.SourceResult) == "{}" {
			published.SourceResult = result.SourceResult
		}
		if published.Fingerprint == "" {
			published.Fingerprint = result.Fingerprint
		}
		if published.StateFile == "" {
			published.StateFile = result.StateFile
		}
		published.Channel = result.Channel
		result = published
	}

	outputPath := getenv("GITHUB_OUTPUT")
	if cliMode {
		outputPath = ""
	}
	if outputPath == "" {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintln(stderr, "unable to encode Action result")
			return 1
		}
	} else if err := appendFile(outputPath, func(writer io.Writer) error { return githubio.WriteOutputs(writer, result) }); err != nil {
		fmt.Fprintln(stderr, "unable to write GitHub outputs")
		return 1
	}
	if summaryPath := getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" && !cliMode {
		if err := appendFile(summaryPath, func(writer io.Writer) error { return githubio.WriteStepSummary(writer, result) }); err != nil {
			fmt.Fprintln(stderr, "unable to write GitHub step summary")
			return 1
		}
	}
	return 0
}

func followupPublishInput(enabled bool, input action.Input, result action.Result) (action.Input, bool) {
	if !enabled || input.DryRun || result.Operation != string(action.OperationCheck) || !result.Changed ||
		result.UpdateStrategy != "publish" || !result.OfficialStoreEnabled || result.OfficialReviewPending || result.LPKPath == "" {
		return action.Input{}, false
	}
	changelog := input.Changelog
	if changelog == "" {
		changelog = "Release " + result.Version
	}
	return action.Input{
		Operation:           action.OperationPublishOfficial,
		ConfigPath:          input.ConfigPath,
		Version:             result.Version,
		Tag:                 result.Tag,
		Changelog:           changelog,
		LPKPath:             result.LPKPath,
		ExpectedSHA256:      result.SHA256,
		GuardOfficialReview: true,
	}, true
}

func appendFile(filename string, write func(io.Writer) error) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	writeErr := write(file)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
