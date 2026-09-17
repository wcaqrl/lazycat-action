package main

import (
	"context"
	"encoding/json"
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
	if len(args) > 0 {
		if len(args) == 1 && args[0] == "--version" {
			if err := json.NewEncoder(stdout).Encode(version.Info()); err != nil {
				fmt.Fprintln(stderr, "unable to encode version information")
				return 1
			}
			return 0
		}
		fmt.Fprintln(stderr, "usage: lazycat-action [--version]")
		return 2
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

	outputPath := getenv("GITHUB_OUTPUT")
	if outputPath == "" {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintln(stderr, "unable to encode Action result")
			return 1
		}
	} else if err := appendFile(outputPath, func(writer io.Writer) error { return githubio.WriteOutputs(writer, result) }); err != nil {
		fmt.Fprintln(stderr, "unable to write GitHub outputs")
		return 1
	}
	if summaryPath := getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		if err := appendFile(summaryPath, func(writer io.Writer) error { return githubio.WriteStepSummary(writer, result) }); err != nil {
			fmt.Fprintln(stderr, "unable to write GitHub step summary")
			return 1
		}
	}
	return 0
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
