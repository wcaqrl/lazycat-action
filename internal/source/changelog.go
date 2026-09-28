package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	if strings.EqualFold(strings.TrimSpace(settings.Mode), "github-release") {
		return runner.githubReleaseChangelog(ctx, settings.GitURL, candidate.Tag, targetVersion)
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

func (runner GitRunner) githubReleaseChangelog(ctx context.Context, gitURL, tag, targetVersion string) (string, error) {
	owner, repository, err := githubRepository(gitURL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(tag) == "" {
		return "", errors.New("GitHub release changelog requires a release tag")
	}
	base := strings.TrimRight(strings.TrimSpace(runner.GitHubAPIBase), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", base, url.PathEscape(owner), url.PathEscape(repository), url.PathEscape(tag))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "lazycat-action")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if runner.Getenv != nil {
		if token := strings.TrimSpace(runner.Getenv("GITHUB_TOKEN")); token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
	}
	client := runner.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("read GitHub release %q: %w", tag, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("read GitHub release %q: HTTP %d: %s", tag, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&release); err != nil {
		return "", fmt.Errorf("decode GitHub release %q: %w", tag, err)
	}
	if release.Draft || release.Prerelease {
		return "", fmt.Errorf("GitHub release %q is not a stable published release", tag)
	}
	if release.TagName != tag {
		return "", fmt.Errorf("GitHub release tag mismatch: requested %q, received %q", tag, release.TagName)
	}
	body := strings.TrimSpace(strings.ReplaceAll(release.Body, "\r\n", "\n"))
	if body == "" {
		return "", fmt.Errorf("GitHub release %q has an empty body", tag)
	}
	const maximumRunes = 12000
	runes := []rune(body)
	if len(runes) > maximumRunes {
		body = string(runes[:maximumRunes]) + "\n\n…"
	}
	return "Upstream " + targetVersion + "\n" + body, nil
}

func githubRepository(raw string) (string, string, error) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "git@github.com:") {
		value = "https://github.com/" + strings.TrimPrefix(value, "git@github.com:")
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", errors.New("github-release changelog requires a github.com Git URL")
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("github-release changelog requires an owner/repository Git URL")
	}
	return parts[0], parts[1], nil
}
