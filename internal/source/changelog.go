package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/config"
)

var commitIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{40,64}$`)

// Changelog builds release notes from the commits between upstream releases.
// The source revision is used only for Git sources; an OCI digest is never a Git commit.
func (runner GitRunner) Changelog(ctx context.Context, settings config.Changelog, candidate, previous Candidate, currentVersion, targetVersion string) (string, error) {
	if strings.TrimSpace(settings.GitURL) == "" {
		return "", nil
	}
	if parsed, err := url.Parse(settings.GitURL); err == nil && parsed.User != nil {
		return "", errors.New("changelog Git URL must not contain credentials")
	}
	if !commitIDPattern.MatchString(candidate.Revision) && candidate.Tag == "" {
		return "", errors.New("changelog requires a source Git revision or a release tag")
	}
	clean, environment, err := runner.authentication(settings.AuthRef)
	if err != nil {
		return "", err
	}
	defer clean()
	directory, err := os.MkdirTemp("", "lazycat-changelog-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	repository := filepath.Join(directory, "repository")
	if _, err := runner.run(ctx, []string{"clone", "--quiet", "--filter=blob:none", "--no-checkout", settings.GitURL, repository}, environment); err != nil {
		return "", fmt.Errorf("clone changelog Git repository: %w", err)
	}
	resolve := func(reference string) (string, error) {
		output, err := runner.run(ctx, []string{"-C", repository, "rev-parse", "--verify", "--end-of-options", reference + "^{commit}"}, environment)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(output)), nil
	}
	target := ""
	if candidate.Kind == string(config.SourceKindGit) && commitIDPattern.MatchString(candidate.Revision) {
		target, err = resolve(candidate.Revision)
	} else {
		for _, tag := range []string{candidate.Tag, "v" + strings.TrimPrefix(candidate.Tag, "v")} {
			target, err = resolve("refs/tags/" + tag)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("resolve changelog target %q: %w", candidate.Tag, err)
	}
	base := ""
	if previous.Kind == string(config.SourceKindGit) && commitIDPattern.MatchString(previous.Revision) {
		base, err = resolve(previous.Revision)
		if err != nil {
			return "", fmt.Errorf("resolve previous Git revision: %w", err)
		}
	} else if currentVersion != "" {
		for _, tag := range []string{"v" + currentVersion, currentVersion} {
			base, err = resolve("refs/tags/" + tag)
			if err == nil {
				break
			}
		}
		// A pre-existing app version need not have an upstream Git tag.
		// In that case we describe the latest commit rather than invent a range.
	}
	maximum := settings.MaxCommits
	if maximum == 0 {
		maximum = 20
	}
	if maximum < 1 || maximum > 50 {
		return "", errors.New("changelog max_commits must be between 1 and 50")
	}
	limit := "--max-count=" + strconv.Itoa(maximum)
	args := []string{"-C", repository, "log", "--no-merges", "--format=%s", limit}
	if base != "" && base != target {
		args = append(args, base+".."+target)
	} else {
		args = append(args, target)
		args = append(args, "--max-count=1")
	}
	output, err := runner.run(ctx, args, environment)
	if err != nil {
		return "", fmt.Errorf("read upstream Git history: %w", err)
	}
	lines := make([]string, 0, maximum)
	for _, line := range strings.Split(string(output), "\n") {
		message := strings.TrimSpace(strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return -1
			}
			return r
		}, line))
		if message == "" {
			continue
		}
		if len([]rune(message)) > 250 {
			message = string([]rune(message)[:250]) + "…"
		}
		lines = append(lines, "- "+message)
	}
	if len(lines) == 0 {
		return "", errors.New("no Git commits found for the selected changelog range")
	}
	header := "Upstream " + targetVersion
	if base != "" && base != target {
		header += " (since " + currentVersion + ")"
	}
	return header + "\n" + strings.Join(lines, "\n"), nil
}
