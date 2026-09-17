package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sort"

	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
)

type streamingShellRunner struct {
	stdout io.Writer
	stderr io.Writer
}

func (runner streamingShellRunner) Run(ctx context.Context, command toolkitbuild.Command) error {
	if ctx == nil {
		return errors.New("buildscript context is required")
	}
	if command.Script == "" {
		return errors.New("buildscript is empty")
	}
	name, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		name, flag = "cmd", "/c"
	}
	process := exec.CommandContext(ctx, name, flag, command.Script)
	process.Dir = command.Dir
	process.Stdout = runner.stdout
	process.Stderr = runner.stderr
	keys := make([]string, 0, len(command.Env))
	for key := range command.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	process.Env = make([]string, 0, len(keys))
	for _, key := range keys {
		process.Env = append(process.Env, key+"="+command.Env[key])
	}
	if err := process.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("buildscript exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("run buildscript: %w", err)
	}
	return nil
}
