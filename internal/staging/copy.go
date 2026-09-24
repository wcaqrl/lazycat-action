package staging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/platform"
)

// Copy mirrors one immutable platform image through a registry that the
// LazyCat copy service can read without source credentials.
func Copy(ctx context.Context, source, destination string, target platform.Target) error {
	if ctx == nil {
		return errors.New("staging context is required")
	}
	source = strings.TrimSpace(source)
	destination = strings.TrimSpace(destination)
	if source == "" || destination == "" {
		return errors.New("staging source and destination are required")
	}
	crane, err := exec.LookPath("crane")
	if err != nil {
		return errors.New("crane is required for staging_image delivery")
	}
	command := exec.CommandContext(ctx, crane, "copy", "--platform", target.Platform(), "--jobs", "1", source, destination)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("stage image with crane: %w", err)
	}
	return nil
}
