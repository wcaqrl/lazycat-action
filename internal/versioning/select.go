package versioning

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/wcaqrl/lazycat-action/internal/appversion"
)

var versionTemplatePlaceholderPattern = regexp.MustCompile(`\{[A-Za-z][A-Za-z0-9_]*\}`)

type Channel string

const (
	ChannelStable  Channel = "stable"
	ChannelBeta    Channel = "beta"
	ChannelDate    Channel = "date"
	ChannelNightly Channel = "nightly"
	ChannelCustom  Channel = "custom"
)

type Sort string

const (
	SortSemVer  Sort = "semver"
	SortCreated Sort = "created"
	SortUpdated Sort = "updated"
)

type Rule struct {
	Channel         Channel
	Sort            Sort
	TagRegex        *regexp.Regexp
	ExcludeRegex    *regexp.Regexp
	VersionRegex    *regexp.Regexp
	VersionTemplate string
}

type Candidate struct {
	Tag     string
	Digest  string
	Created time.Time
	Updated time.Time
}

type Selection struct {
	Candidate Candidate
	Version   string
}

type ranked struct {
	candidate Candidate
	version   string
	semver    *semver.Version
}

func Select(rule Rule, candidates []Candidate) (Selection, error) {
	if err := validateRule(rule); err != nil {
		return Selection{}, err
	}
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if rule.TagRegex != nil && !rule.TagRegex.MatchString(candidate.Tag) {
			continue
		}
		if rule.ExcludeRegex != nil && rule.ExcludeRegex.MatchString(candidate.Tag) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return Selection{}, errors.New("no image tag matches the configured channel")
	}
	if rule.Channel == ChannelNightly {
		return selectNightly(filtered)
	}
	rankedCandidates, err := rankMappedCandidates(rule, filtered)
	if err != nil {
		return Selection{}, err
	}
	return rankedCandidates[0], nil
}

func SelectMutable(rule Rule, candidates []Candidate) (Selection, error) {
	if err := validateRule(rule); err != nil {
		return Selection{}, err
	}
	if rule.Channel != ChannelCustom || rule.Sort != SortCreated || rule.TagRegex == nil || rule.VersionRegex != nil {
		return Selection{}, errors.New("mutable selection requires a custom created rule without version mapping")
	}
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !rule.TagRegex.MatchString(candidate.Tag) || rule.ExcludeRegex != nil && rule.ExcludeRegex.MatchString(candidate.Tag) {
			continue
		}
		if candidate.Created.IsZero() {
			return Selection{}, fmt.Errorf("image tag %q creation time is required", candidate.Tag)
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return Selection{}, errors.New("no image tag matches the configured mutable channel")
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Created.Equal(filtered[j].Created) {
			return filtered[i].Tag > filtered[j].Tag
		}
		return filtered[i].Created.After(filtered[j].Created)
	})
	return Selection{Candidate: filtered[0]}, nil
}

func BumpPatch(value string) (string, error) {
	parsed, err := semver.StrictNewVersion(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("parse current version %q: %w", value, err)
	}
	if parsed.Prerelease() != "" || parsed.Metadata() != "" {
		return "", fmt.Errorf("current version %q must be stable SemVer without metadata", value)
	}
	if parsed.Patch() == math.MaxUint64 {
		return "", fmt.Errorf("current version %q patch component overflows", value)
	}
	return fmt.Sprintf("%d.%d.%d", parsed.Major(), parsed.Minor(), parsed.Patch()+1), nil
}

// RankSemVer maps and orders tag-only candidates without requiring image
// manifests. Callers can inspect the ranked tags until a usable platform is
// found instead of fetching metadata for every matching release.
func RankSemVer(rule Rule, candidates []Candidate) ([]Selection, error) {
	if err := validateRule(rule); err != nil {
		return nil, err
	}
	if rule.Sort != SortSemVer || rule.Channel == ChannelNightly {
		return nil, errors.New("SemVer ranking requires a stable, beta, date, or custom semver rule")
	}
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if rule.TagRegex != nil && !rule.TagRegex.MatchString(candidate.Tag) {
			continue
		}
		if rule.ExcludeRegex != nil && rule.ExcludeRegex.MatchString(candidate.Tag) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, errors.New("no image tag matches the configured channel")
	}
	return rankMappedCandidates(rule, filtered)
}

// RankUpdated maps and orders tag-only candidates using registry tag update
// metadata. Callers can inspect manifests in rank order until a usable target
// platform is found.
func RankUpdated(rule Rule, candidates []Candidate) ([]Selection, error) {
	if err := validateRule(rule); err != nil {
		return nil, err
	}
	if rule.Sort != SortUpdated || rule.Channel == ChannelNightly {
		return nil, errors.New("updated ranking requires a stable, beta, date, or custom updated rule")
	}
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if rule.TagRegex != nil && !rule.TagRegex.MatchString(candidate.Tag) {
			continue
		}
		if rule.ExcludeRegex != nil && rule.ExcludeRegex.MatchString(candidate.Tag) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, errors.New("no image tag matches the configured channel")
	}
	return rankMappedCandidates(rule, filtered)
}

func rankMappedCandidates(rule Rule, filtered []Candidate) ([]Selection, error) {
	rankedCandidates := make([]ranked, 0, len(filtered))
	for _, candidate := range filtered {
		if rule.Sort == SortUpdated && candidate.Updated.IsZero() {
			return nil, fmt.Errorf("image tag %q update time is required", candidate.Tag)
		}
		mapped, err := mapVersion(rule, candidate.Tag)
		if err != nil {
			if rule.Channel == ChannelStable || rule.Channel == ChannelBeta || rule.Channel == ChannelDate {
				continue
			}
			return nil, err
		}
		parsed, err := semver.StrictNewVersion(mapped)
		if err != nil {
			return nil, fmt.Errorf("parse mapped version %q: %w", mapped, err)
		}
		switch rule.Channel {
		case ChannelStable, ChannelDate:
			if parsed.Prerelease() != "" {
				continue
			}
		case ChannelBeta:
			if parsed.Prerelease() == "" {
				continue
			}
		}
		rankedCandidates = append(rankedCandidates, ranked{candidate: candidate, version: mapped, semver: parsed})
	}
	if len(rankedCandidates) == 0 {
		return nil, errors.New("no valid image version matches the configured channel")
	}
	sort.SliceStable(rankedCandidates, func(i, j int) bool {
		if rule.Sort == SortCreated {
			if rankedCandidates[i].candidate.Created.Equal(rankedCandidates[j].candidate.Created) {
				return rankedCandidates[i].candidate.Tag > rankedCandidates[j].candidate.Tag
			}
			return rankedCandidates[i].candidate.Created.After(rankedCandidates[j].candidate.Created)
		}
		if rule.Sort == SortUpdated && !rankedCandidates[i].candidate.Updated.Equal(rankedCandidates[j].candidate.Updated) {
			return rankedCandidates[i].candidate.Updated.After(rankedCandidates[j].candidate.Updated)
		}
		comparison := rankedCandidates[i].semver.Compare(rankedCandidates[j].semver)
		if comparison == 0 {
			return rankedCandidates[i].candidate.Tag > rankedCandidates[j].candidate.Tag
		}
		return comparison > 0
	})
	selections := make([]Selection, 0, len(rankedCandidates))
	for _, selected := range rankedCandidates {
		selections = append(selections, Selection{Candidate: selected.candidate, Version: selected.version})
	}
	return selections, nil
}

func validateRule(rule Rule) error {
	switch rule.Channel {
	case ChannelStable, ChannelBeta, ChannelDate:
		if rule.Sort != SortSemVer && rule.Sort != SortUpdated {
			return fmt.Errorf("channel %q requires semver or updated sorting", rule.Channel)
		}
	case ChannelNightly:
		if rule.Sort != SortCreated || rule.TagRegex == nil {
			return errors.New("nightly channel requires created sorting and tag regex")
		}
	case ChannelCustom:
		if rule.TagRegex == nil || (rule.Sort != SortSemVer && rule.Sort != SortCreated && rule.Sort != SortUpdated) {
			return errors.New("custom channel requires tag regex and explicit sorting")
		}
	default:
		return fmt.Errorf("unsupported channel %q", rule.Channel)
	}
	if rule.VersionRegex != nil && rule.VersionRegex.SubexpIndex("version") < 0 {
		return errors.New("version_regex must define a named version group")
	}
	return nil
}

func mapVersion(rule Rule, tag string) (string, error) {
	value := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	groups := map[string]string{"version": value}
	if rule.VersionRegex != nil {
		matches := rule.VersionRegex.FindStringSubmatch(tag)
		if matches == nil {
			return "", fmt.Errorf("tag %q does not match version_regex", tag)
		}
		for index, name := range rule.VersionRegex.SubexpNames() {
			if index == 0 || name == "" {
				continue
			}
			groups[name] = matches[index]
		}
	}
	template := rule.VersionTemplate
	if template == "" {
		template = "{version}"
	}
	value = template
	for name, match := range groups {
		value = strings.ReplaceAll(value, "{"+name+"}", match)
	}
	if placeholder := versionTemplatePlaceholderPattern.FindString(value); placeholder != "" {
		return "", fmt.Errorf("unresolved version template placeholder %q for tag %q", placeholder, tag)
	}
	// Explicit regex/template mappings often assemble SemVer components from
	// fixed-width fields. Canonicalize the core numeric components before the
	// strict SemVer check so date tags such as 20260626 map to 2026.6.26.
	if rule.VersionRegex != nil {
		value = normalizeSemVerCore(value)
	}
	if !appversion.IsValid(value) {
		return "", fmt.Errorf("mapped version %q from tag %q is not valid SemVer", value, tag)
	}
	return value, nil
}

func normalizeSemVerCore(value string) string {
	coreEnd := strings.IndexAny(value, "-+")
	if coreEnd < 0 {
		coreEnd = len(value)
	}
	core := value[:coreEnd]
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return value
	}
	for index, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return value
		}
		part = strings.TrimLeft(part, "0")
		if part == "" {
			part = "0"
		}
		parts[index] = part
	}
	return strings.Join(parts, ".") + value[coreEnd:]
}

func selectNightly(candidates []Candidate) (Selection, error) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Created.Equal(candidates[j].Created) {
			return candidates[i].Tag > candidates[j].Tag
		}
		return candidates[i].Created.After(candidates[j].Created)
	})
	selected := candidates[0]
	digest := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(selected.Digest)), "sha256:")
	if matched, _ := regexp.MatchString(`^[0-9a-f]{64}$`, digest); !matched {
		return Selection{}, fmt.Errorf("nightly digest %q must be a sha256 digest", selected.Digest)
	}
	if selected.Created.IsZero() {
		return Selection{}, errors.New("nightly image creation time is required")
	}
	version := fmt.Sprintf("0.0.0-nightly.%s.%s", selected.Created.UTC().Format("20060102150405"), digest[:12])
	return Selection{Candidate: selected, Version: version}, nil
}
