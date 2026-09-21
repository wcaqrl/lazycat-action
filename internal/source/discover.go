package source

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/wcaqrl/lazycat-action/internal/appversion"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/registry"
	"github.com/wcaqrl/lazycat-action/internal/versioning"
)

var authRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type Candidate struct {
	Kind     string `json:"kind" yaml:"kind"`
	Version  string `json:"version,omitempty" yaml:"version,omitempty"`
	Ref      string `json:"ref" yaml:"ref"`
	Revision string `json:"revision" yaml:"revision"`
	Branch   string `json:"branch,omitempty" yaml:"branch,omitempty"`
	Tag      string `json:"tag,omitempty" yaml:"tag,omitempty"`
	Image    string `json:"image,omitempty" yaml:"image,omitempty"`
}

type Request struct {
	Source         config.Source
	Target         platform.Target
	CurrentVersion string
}

type Discoverer struct {
	Registry *registry.Client
	Git      GitRunner
}

func Default(getenv func(string) string) Discoverer {
	return Discoverer{Registry: registry.New(), Git: GitRunner{Getenv: getenv}}
}

func (discoverer Discoverer) Discover(ctx context.Context, request Request) (Candidate, error) {
	if ctx == nil {
		return Candidate{}, errors.New("source discovery context is required")
	}
	switch request.Source.Kind {
	case config.SourceKindGit:
		return discoverer.Git.Discover(ctx, request.Source, request.CurrentVersion)
	case config.SourceKindOCI:
		return discoverer.discoverOCI(ctx, request)
	default:
		return Candidate{}, fmt.Errorf("unsupported source kind %q", request.Source.Kind)
	}
}

func (discoverer Discoverer) discoverOCI(ctx context.Context, request Request) (Candidate, error) {
	if discoverer.Registry == nil {
		return Candidate{}, errors.New("OCI source discovery requires a registry client")
	}
	include, err := compileOptional(request.Source.Select.TagRegex)
	if err != nil {
		return Candidate{}, fmt.Errorf("compile source tag_regex: %w", err)
	}
	exclude, err := compileOptional(request.Source.Select.ExcludeRegex)
	if err != nil {
		return Candidate{}, fmt.Errorf("compile source exclude_regex: %w", err)
	}
	rule := versioning.Rule{
		Channel:         versioning.ChannelStable,
		Sort:            versioning.SortSemVer,
		TagRegex:        include,
		ExcludeRegex:    exclude,
		VersionTemplate: "{version}",
	}
	channel := strings.ToLower(strings.TrimSpace(request.Source.Select.Channel))
	if channel != "" {
		rule.Channel = versioning.Channel(channel)
	}
	sortMode := strings.ToLower(strings.TrimSpace(request.Source.Select.Sort))
	if sortMode != "" {
		rule.Sort = versioning.Sort(sortMode)
	}
	filter := registry.TagFilter{Include: include, Exclude: exclude}
	if rule.Sort == versioning.SortSemVer {
		filter.SemVerRule = &rule
	}
	candidates, err := discoverer.Registry.CandidatesForTarget(ctx, request.Source.Image, request.Target, filter)
	if err != nil {
		return Candidate{}, fmt.Errorf("discover OCI source: %w", err)
	}
	selected, err := versioning.Select(rule, candidates)
	if err != nil {
		return Candidate{}, fmt.Errorf("select OCI source: %w", err)
	}
	return Candidate{
		Kind: string(config.SourceKindOCI), Version: selected.Version,
		Ref:      request.Source.Image + ":" + selected.Candidate.Tag,
		Revision: selected.Candidate.Digest, Tag: selected.Candidate.Tag,
		Image: request.Source.Image,
	}, nil
}

type GitRunner struct {
	Getenv func(string) string
	Run    func(context.Context, []string, []string) ([]byte, error)
}

func (runner GitRunner) Checkout(ctx context.Context, source config.Source, candidate Candidate, destination string) error {
	if source.Kind != config.SourceKindGit {
		return errors.New("checkout requires a Git source")
	}
	if strings.TrimSpace(candidate.Revision) == "" {
		return errors.New("checkout requires an immutable Git revision")
	}
	cleanup, environment, err := runner.authentication(source.AuthRef)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := runner.run(ctx, []string{"clone", "--no-checkout", "--filter=blob:none", source.URL, destination}, environment); err != nil {
		return fmt.Errorf("clone Git source: %w", err)
	}
	if _, err := runner.run(ctx, []string{"-C", destination, "checkout", "--detach", candidate.Revision}, environment); err != nil {
		return fmt.Errorf("checkout Git source revision: %w", err)
	}
	return nil
}

func (runner GitRunner) Discover(ctx context.Context, source config.Source, currentVersion string) (Candidate, error) {
	if strings.TrimSpace(source.URL) == "" {
		return Candidate{}, errors.New("Git source URL is required")
	}
	if parsed, err := url.Parse(source.URL); err == nil && parsed.User != nil {
		return Candidate{}, errors.New("Git source URL must not contain credentials")
	}
	cleanup, environment, err := runner.authentication(source.AuthRef)
	if err != nil {
		return Candidate{}, err
	}
	defer cleanup()
	strategy := strings.ToLower(strings.TrimSpace(source.Select.Strategy))
	if strategy == "" {
		strategy = "branch-head"
	}
	switch strategy {
	case "branch", "branch-head", "default-branch":
		return runner.discoverBranch(ctx, source, currentVersion, environment)
	case "tag", "release", "semver-tag":
		return runner.discoverTag(ctx, source, environment)
	default:
		return Candidate{}, fmt.Errorf("unsupported Git source strategy %q", strategy)
	}
}

func (runner GitRunner) discoverBranch(ctx context.Context, source config.Source, _ string, environment []string) (Candidate, error) {
	branch := strings.TrimSpace(source.Select.Branch)
	if branch == "" || strings.EqualFold(branch, "auto") {
		output, err := runner.run(ctx, []string{"ls-remote", "--symref", source.URL, "HEAD"}, environment)
		if err != nil {
			return Candidate{}, fmt.Errorf("resolve Git default branch: %w", err)
		}
		branch, _ = parseSymrefHEAD(output)
		if branch == "" {
			return Candidate{}, errors.New("remote Git HEAD does not identify a default branch")
		}
	}
	output, err := runner.run(ctx, []string{"ls-remote", source.URL, "refs/heads/" + branch}, environment)
	if err != nil {
		return Candidate{}, fmt.Errorf("resolve Git branch %q: %w", branch, err)
	}
	revision := parseExactRef(output, "refs/heads/"+branch)
	if revision == "" {
		return Candidate{}, fmt.Errorf("Git branch %q was not found", branch)
	}
	return Candidate{Kind: string(config.SourceKindGit), Ref: "refs/heads/" + branch, Revision: revision, Branch: branch}, nil
}

func (runner GitRunner) discoverTag(ctx context.Context, source config.Source, environment []string) (Candidate, error) {
	output, err := runner.run(ctx, []string{"ls-remote", "--tags", source.URL}, environment)
	if err != nil {
		return Candidate{}, fmt.Errorf("list Git tags: %w", err)
	}
	include, err := compileOptional(source.Select.TagRegex)
	if err != nil {
		return Candidate{}, fmt.Errorf("compile source tag_regex: %w", err)
	}
	exclude, err := compileOptional(source.Select.ExcludeRegex)
	if err != nil {
		return Candidate{}, fmt.Errorf("compile source exclude_regex: %w", err)
	}
	tags := parseTags(output)
	type rankedTag struct {
		name     string
		revision string
		version  *semver.Version
	}
	ranked := make([]rankedTag, 0, len(tags))
	for name, revision := range tags {
		if include != nil && !include.MatchString(name) {
			continue
		}
		if exclude != nil && exclude.MatchString(name) {
			continue
		}
		version, _, normalizeErr := appversion.Normalize(name)
		if normalizeErr != nil {
			continue
		}
		parsed, parseErr := semver.StrictNewVersion(version)
		if parseErr != nil || parsed.Prerelease() != "" {
			continue
		}
		ranked = append(ranked, rankedTag{name: name, revision: revision, version: parsed})
	}
	if len(ranked) == 0 {
		return Candidate{}, errors.New("no stable SemVer Git tag matched the configured source")
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].version.GreaterThan(ranked[j].version) })
	selected := ranked[0]
	return Candidate{
		Kind: string(config.SourceKindGit), Version: selected.version.String(),
		Ref: "refs/tags/" + selected.name, Revision: selected.revision, Tag: selected.name,
	}, nil
}

func (runner GitRunner) run(ctx context.Context, args, environment []string) ([]byte, error) {
	if runner.Run != nil {
		return runner.Run(ctx, args, environment)
	}
	command := exec.CommandContext(ctx, "git", args...)
	command.Env = append(os.Environ(), environment...)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return nil, fmt.Errorf("git exited with status %d", exitError.ExitCode())
		}
		return nil, err
	}
	return output, nil
}

func (runner GitRunner) authentication(authRef string) (func(), []string, error) {
	authRef = strings.TrimSpace(authRef)
	if authRef == "" {
		return func() {}, []string{"GIT_TERMINAL_PROMPT=0"}, nil
	}
	if !authRefPattern.MatchString(authRef) {
		return nil, nil, fmt.Errorf("source auth_ref %q contains unsupported characters", authRef)
	}
	getenv := runner.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	prefix := "LAZYCAT_AUTH_" + strings.ToUpper(strings.ReplaceAll(authRef, "-", "_"))
	privateKey := getenv(prefix + "_SSH_KEY")
	token := getenv(prefix + "_TOKEN")
	if privateKey != "" && token != "" {
		return nil, nil, fmt.Errorf("source auth_ref %q defines both SSH key and token", authRef)
	}
	if privateKey == "" && token == "" {
		return nil, nil, fmt.Errorf("source auth_ref %q has no %s_SSH_KEY or %s_TOKEN secret", authRef, prefix, prefix)
	}
	directory, err := os.MkdirTemp("", "lazycat-source-auth-")
	if err != nil {
		return nil, nil, fmt.Errorf("create source authentication directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	if privateKey != "" {
		keyPath := filepath.Join(directory, "key")
		if err := os.WriteFile(keyPath, []byte(privateKey), 0o600); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("write source SSH key: %w", err)
		}
		sshParts := []string{"ssh", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-i", keyPath}
		if knownHosts := getenv(prefix + "_KNOWN_HOSTS"); knownHosts != "" {
			knownHostsPath := filepath.Join(directory, "known_hosts")
			if err := os.WriteFile(knownHostsPath, []byte(knownHosts), 0o600); err != nil {
				cleanup()
				return nil, nil, fmt.Errorf("write source known_hosts: %w", err)
			}
			sshParts = append(sshParts, "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+knownHostsPath)
		}
		return cleanup, []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=" + strings.Join(sshParts, " ")}, nil
	}
	askpassPath := filepath.Join(directory, "askpass.sh")
	askpass := "#!/bin/sh\ncase \"$1\" in\n  *sername*) printf '%s\\n' \"${LAZYCAT_GIT_USERNAME:-oauth2}\" ;;\n  *) printf '%s\\n' \"$LAZYCAT_GIT_TOKEN\" ;;\nesac\n"
	if err := os.WriteFile(askpassPath, []byte(askpass), 0o700); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("write source token helper: %w", err)
	}
	username := getenv(prefix + "_USERNAME")
	return cleanup, []string{
		"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=" + askpassPath,
		"LAZYCAT_GIT_TOKEN=" + token, "LAZYCAT_GIT_USERNAME=" + username,
	}, nil
}

func parseSymrefHEAD(output []byte) (string, string) {
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	branch := ""
	revision := ""
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
			branch = strings.TrimPrefix(fields[1], "refs/heads/")
		}
		if len(fields) == 2 && fields[1] == "HEAD" {
			revision = fields[0]
		}
	}
	return branch, revision
}

func parseExactRef(output []byte, reference string) string {
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == reference {
			return fields[0]
		}
	}
	return ""
}

func parseTags(output []byte) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "refs/tags/") {
			continue
		}
		name := strings.TrimPrefix(fields[1], "refs/tags/")
		peeled := strings.HasSuffix(name, "^{}")
		name = strings.TrimSuffix(name, "^{}")
		if _, exists := result[name]; !exists || peeled {
			result[name] = fields[0]
		}
	}
	return result
}

func compileOptional(pattern string) (*regexp.Regexp, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}
