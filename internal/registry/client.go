package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/versioning"
)

const (
	defaultMaxTags         = 10000
	defaultMaxMatchingTags = 10000
)

var ErrPlatformNotFound = errors.New("target image platform not found")

type publicError struct {
	detail string
}

func (err publicError) Error() string {
	return err.detail
}

func (err publicError) PublicErrorDetail() string {
	return err.detail
}

type Image struct {
	Reference string    `json:"reference"`
	Digest    string    `json:"digest"`
	Created   time.Time `json:"created"`
	Platform  string    `json:"platform"`
}

type Client struct {
	options     []remote.Option
	tagMetadata tagMetadata
}

type TagFilter struct {
	Include         *regexp.Regexp
	Exclude         *regexp.Regexp
	SemVerRule      *versioning.Rule
	UpdatedRule     *versioning.Rule
	MaxTags         int
	MaxMatchingTags int
}

func New(options ...remote.Option) *Client {
	return &Client{
		options: append([]remote.Option{remote.WithAuthFromKeychain(authn.DefaultKeychain)}, options...),
		tagMetadata: dockerHubTagMetadata{
			client:  &http.Client{Timeout: dockerHubRequestTimeout},
			baseURL: dockerHubBaseURL,
		},
	}
}

func (client *Client) Candidates(ctx context.Context, source string, filters ...TagFilter) ([]versioning.Candidate, error) {
	target, _ := platform.NormalizeTarget("")
	return client.CandidatesForTarget(ctx, source, target, filters...)
}

func (client *Client) CandidatesForTarget(ctx context.Context, source string, target platform.Target, filters ...TagFilter) ([]versioning.Candidate, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	target, err := target.Normalize()
	if err != nil {
		return nil, err
	}
	repository, err := name.NewRepository(strings.TrimSpace(source), name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("parse image repository %q: %w", source, err)
	}
	var filter TagFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	tags, err := remote.List(repository, client.remoteOptions(ctx, target)...)
	if err != nil {
		return nil, fmt.Errorf("list image tags for %q: %w", source, err)
	}
	maxTags := filter.MaxTags
	if maxTags == 0 {
		maxTags = defaultMaxTags
	}
	if len(tags) > maxTags {
		return nil, publicError{detail: fmt.Sprintf("image repository returned %d tags; raw limit is %d", len(tags), maxTags)}
	}
	sort.Strings(tags)
	eligibleTags, err := filterTags(source, tags, filter)
	if err != nil {
		return nil, err
	}
	if filter.SemVerRule != nil {
		tagCandidates := make([]versioning.Candidate, 0, len(eligibleTags))
		for _, tag := range eligibleTags {
			tagCandidates = append(tagCandidates, versioning.Candidate{Tag: tag})
		}
		ranked, err := versioning.RankSemVer(*filter.SemVerRule, tagCandidates)
		if err != nil {
			// Preserve version-selection error semantics in imageflow. SemVer
			// ranking does not need manifest metadata, so tag-only candidates
			// are sufficient for the caller to report the original rule error.
			return tagCandidates, nil
		}
		missingPlatform := 0
		for _, selection := range ranked {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			image, err := client.InspectTarget(ctx, repository.Tag(selection.Candidate.Tag).Name(), target)
			if err != nil {
				if errors.Is(err, ErrPlatformNotFound) {
					missingPlatform++
					continue
				}
				return nil, fmt.Errorf("inspect image tag %q: %w", selection.Candidate.Tag, err)
			}
			return []versioning.Candidate{{Tag: selection.Candidate.Tag, Digest: image.Digest, Created: image.Created}}, nil
		}
		if missingPlatform > 0 {
			return nil, fmt.Errorf("%w for repository %q", ErrPlatformNotFound, source)
		}
		return nil, nil
	}
	if filter.UpdatedRule != nil {
		metadata := clientTagMetadata(client)
		updates, err := metadata.Updates(ctx, repository, eligibleTags, matchingTagLimit(filter))
		if err != nil {
			return nil, fmt.Errorf("read image tag update times for %q: %w", source, err)
		}
		tagCandidates := make([]versioning.Candidate, 0, len(eligibleTags))
		for _, tag := range eligibleTags {
			tagCandidates = append(tagCandidates, versioning.Candidate{Tag: tag, Updated: updates[tag]})
		}
		ranked, err := versioning.RankUpdated(*filter.UpdatedRule, tagCandidates)
		if err != nil {
			return tagCandidates, nil
		}
		missingPlatform := 0
		for _, selection := range ranked {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			image, err := client.InspectTarget(ctx, repository.Tag(selection.Candidate.Tag).Name(), target)
			if err != nil {
				if errors.Is(err, ErrPlatformNotFound) {
					missingPlatform++
					continue
				}
				return nil, fmt.Errorf("inspect image tag %q: %w", selection.Candidate.Tag, err)
			}
			return []versioning.Candidate{{
				Tag: selection.Candidate.Tag, Digest: image.Digest, Created: image.Created, Updated: selection.Candidate.Updated,
			}}, nil
		}
		if missingPlatform > 0 {
			return nil, fmt.Errorf("%w for repository %q", ErrPlatformNotFound, source)
		}
		return nil, nil
	}
	candidates := make([]versioning.Candidate, 0, len(eligibleTags))
	missingPlatform := 0
	for _, tag := range eligibleTags {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		image, err := client.InspectTarget(ctx, repository.Tag(tag).Name(), target)
		if err != nil {
			if errors.Is(err, ErrPlatformNotFound) {
				missingPlatform++
				continue
			}
			return nil, fmt.Errorf("inspect image tag %q: %w", tag, err)
		}
		candidates = append(candidates, versioning.Candidate{Tag: tag, Digest: image.Digest, Created: image.Created})
	}
	if len(candidates) == 0 && missingPlatform > 0 {
		return nil, fmt.Errorf("%w for repository %q", ErrPlatformNotFound, source)
	}
	return candidates, nil
}

func filterTags(source string, tags []string, filter TagFilter) ([]string, error) {
	eligibleTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		if filter.Include != nil && !filter.Include.MatchString(tag) {
			continue
		}
		if filter.Exclude != nil && filter.Exclude.MatchString(tag) {
			continue
		}
		eligibleTags = append(eligibleTags, tag)
	}
	maxMatchingTags := matchingTagLimit(filter)
	if len(eligibleTags) > maxMatchingTags {
		return nil, publicError{detail: fmt.Sprintf("image repository has %d tags matching the configured filter; limit is %d", len(eligibleTags), maxMatchingTags)}
	}
	return eligibleTags, nil
}

func matchingTagLimit(filter TagFilter) int {
	if filter.MaxMatchingTags > 0 {
		return filter.MaxMatchingTags
	}
	return defaultMaxMatchingTags
}

func clientTagMetadata(client *Client) tagMetadata {
	if client != nil && client.tagMetadata != nil {
		return client.tagMetadata
	}
	return dockerHubTagMetadata{
		client:  &http.Client{Timeout: dockerHubRequestTimeout},
		baseURL: dockerHubBaseURL,
	}
}

func (client *Client) Inspect(ctx context.Context, reference string) (Image, error) {
	target, _ := platform.NormalizeTarget("")
	return client.InspectTarget(ctx, reference, target)
}

func (client *Client) InspectTarget(ctx context.Context, reference string, target platform.Target) (Image, error) {
	if err := checkContext(ctx); err != nil {
		return Image{}, err
	}
	target, err := target.Normalize()
	if err != nil {
		return Image{}, err
	}
	parsed, err := name.ParseReference(strings.TrimSpace(reference), name.WeakValidation)
	if err != nil {
		return Image{}, fmt.Errorf("parse image reference %q: %w", reference, err)
	}
	descriptor, err := remote.Get(parsed, client.remoteOptions(ctx, target)...)
	if err != nil {
		return Image{}, fmt.Errorf("get image manifest %q: %w", reference, err)
	}
	if descriptor.MediaType.IsIndex() {
		index, err := descriptor.ImageIndex()
		if err != nil {
			return Image{}, fmt.Errorf("read image index %q: %w", reference, err)
		}
		manifest, err := index.IndexManifest()
		if err != nil {
			return Image{}, fmt.Errorf("read image index manifest %q: %w", reference, err)
		}
		matched := false
		for _, child := range manifest.Manifests {
			candidate := v1.Platform{OS: target.OS, Architecture: target.Arch}
			if child.Platform != nil {
				candidate = *child.Platform
			}
			if candidate.Satisfies(v1.Platform{OS: target.OS, Architecture: target.Arch}) {
				matched = true
				break
			}
		}
		if !matched {
			return Image{}, fmt.Errorf("%w: image index %q", ErrPlatformNotFound, reference)
		}
	}
	image, err := descriptor.Image()
	if err != nil {
		return Image{}, fmt.Errorf("resolve %s image for %q: %w", target.Platform(), reference, err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		return Image{}, fmt.Errorf("read image config for %q: %w", reference, err)
	}
	if config.OS != target.OS || config.Architecture != target.Arch {
		return Image{}, fmt.Errorf("%w: image %q resolved to %s/%s", ErrPlatformNotFound, reference, config.OS, config.Architecture)
	}
	digest, err := image.Digest()
	if err != nil {
		return Image{}, fmt.Errorf("read image digest for %q: %w", reference, err)
	}
	return Image{
		Reference: parsed.Name(),
		Digest:    digest.String(),
		Created:   config.Created.Time.UTC(),
		Platform:  target.Platform(),
	}, nil
}

func (client *Client) remoteOptions(ctx context.Context, target platform.Target) []remote.Option {
	options := []remote.Option{remote.WithAuthFromKeychain(authn.DefaultKeychain)}
	if client != nil {
		options = append(options, client.options...)
	}
	return append(options, remote.WithContext(ctx), remote.WithPlatform(v1.Platform{OS: target.OS, Architecture: target.Arch}))
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("registry request cancelled: %w", err)
	}
	return nil
}
