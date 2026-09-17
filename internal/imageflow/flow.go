package imageflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/delivery"
	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/registry"
	"github.com/wcaqrl/lazycat-action/internal/versioning"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

var (
	ErrVersionNotFound               = errors.New("image version not found")
	ErrVersionDowngrade              = errors.New("image version downgrade blocked")
	ErrPlatformNotFound              = errors.New("image platform not found")
	ErrDeliveryFailed                = errors.New("image delivery failed")
	ErrMirrorVerificationFailed      = errors.New("mirror verification failed")
	ErrOfficialReviewCoversCandidate = errors.New("official review covers selected candidate")
)

type Registry interface {
	CandidatesForTarget(context.Context, string, platform.Target, ...registry.TagFilter) ([]versioning.Candidate, error)
}

type Deliverer interface {
	Deliver(context.Context, delivery.Request) (delivery.Result, error)
}

type Request struct {
	Config                config.Config
	Project               project.Info
	ImageID               string
	DryRun                bool
	OfficialReviewVersion string
	OnProgress            func(string, appstore.CopyProgress)
}

type LayerProgress struct {
	Hash     string `json:"hash"`
	Progress int    `json:"progress"`
}

type CopyResult struct {
	SourceImage  string          `json:"sourceImage"`
	Platform     string          `json:"platform"`
	LazyCatImage string          `json:"lazyCatImage"`
	Finished     bool            `json:"finished"`
	Layers       []LayerProgress `json:"layers,omitempty"`
}

type ImageResult struct {
	ID              string      `json:"id"`
	Target          string      `json:"target"`
	Service         string      `json:"service,omitempty"`
	Platform        string      `json:"platform"`
	Tag             string      `json:"tag"`
	SourceRef       string      `json:"sourceRef"`
	SourceDigest    string      `json:"sourceDigest"`
	CurrentDigest   string      `json:"currentDigest,omitempty"`
	DigestChanged   bool        `json:"digestChanged"`
	Bump            string      `json:"bump,omitempty"`
	PreviousVersion string      `json:"previousVersion,omitempty"`
	SelectedVersion string      `json:"selectedVersion,omitempty"`
	DeliveryMode    string      `json:"deliveryMode"`
	DeliveredRef    string      `json:"deliveredRef"`
	Copied          bool        `json:"copied"`
	CopyResult      *CopyResult `json:"copyResult,omitempty"`
}

type Result struct {
	Changed bool          `json:"changed"`
	Version string        `json:"version"`
	Channel string        `json:"channel,omitempty"`
	Images  []ImageResult `json:"images"`
}

type Flow struct {
	Registry      Registry
	Deliverer     Deliverer
	ResolveImage  func(config.Image, manifestedit.Current) (config.Image, error)
	ReadManifest  func(string, []manifestedit.Target) ([]manifestedit.Current, error)
	ApplyManifest func(string, []manifestedit.Update) ([]manifestedit.Change, error)
	Logger        *slog.Logger
}

func (flow Flow) Check(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("image check context is required")
	}
	if flow.Registry == nil || flow.Deliverer == nil {
		return Result{}, errors.New("image check requires Registry and delivery adapters")
	}
	selected, err := selectImages(request.Config.Images, request.ImageID)
	if err != nil {
		return Result{}, err
	}
	if request.OfficialReviewVersion != "" {
		selected = prioritizeImage(selected, request.Config.Update.VersionSource.Image)
	}
	logger := flow.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	target := request.Config.Project.Target()
	logger.Info("Docker image update started", "images", len(selected), "dry_run", request.DryRun, "target", target.Platform())
	readManifest := flow.ReadManifest
	if readManifest == nil {
		readManifest = manifestedit.Read
	}
	applyManifest := flow.ApplyManifest
	if applyManifest == nil {
		applyManifest = manifestedit.Apply
	}
	targets := make([]manifestedit.Target, 0, len(selected))
	for _, image := range selected {
		targets = append(targets, imageTarget(image))
	}
	currentValues, err := readManifest(request.Project.ManifestFile, targets)
	if err != nil {
		return Result{}, fmt.Errorf("validate manifest image targets: %w", err)
	}
	currentByID := make(map[string]manifestedit.Current, len(currentValues))
	for _, current := range currentValues {
		currentByID[current.ID] = current
	}
	for _, image := range selected {
		if _, exists := currentByID[image.ID]; !exists {
			return Result{}, fmt.Errorf("manifest reader did not return image target %q", image.ID)
		}
	}
	if flow.ResolveImage != nil {
		for index, image := range selected {
			resolved, err := flow.ResolveImage(image, currentByID[image.ID])
			if err != nil {
				return Result{}, fmt.Errorf("resolve image %q: %w", image.ID, err)
			}
			selected[index] = resolved
		}
	}

	result := Result{Version: request.Project.Version, Images: make([]ImageResult, 0, len(selected))}
	updates := make([]manifestedit.Update, 0, len(selected))
	for _, image := range selected {
		logger.Info("querying Docker image versions", "image_id", image.ID, "source", image.Source, "channel", image.Channel, "sort", image.Sort)
		rule, filter, err := imageRule(image)
		if err != nil {
			return Result{}, err
		}
		candidates, err := flow.Registry.CandidatesForTarget(ctx, image.Source, target, filter)
		if err != nil {
			if errors.Is(err, registry.ErrPlatformNotFound) {
				return Result{}, fmt.Errorf("%w: inspect %q: %v", ErrPlatformNotFound, image.ID, err)
			}
			return Result{}, fmt.Errorf("inspect image %q: %w", image.ID, err)
		}
		logger.Info("Docker image versions received", "image_id", image.ID, "candidates", len(candidates))
		mutable := request.Config.Update.VersionSource.Type == config.VersionSourceImage && request.Config.Update.VersionSource.Image == image.ID && request.Config.Update.VersionSource.Bump == "patch"
		var selection versioning.Selection
		if mutable {
			selection, err = versioning.SelectMutable(rule, candidates)
		} else {
			selection, err = versioning.Select(rule, candidates)
		}
		if err != nil {
			return Result{}, fmt.Errorf("%w for %q: %v", ErrVersionNotFound, image.ID, err)
		}
		logger.Info("Docker image version selected", "image_id", image.ID, "tag", selection.Candidate.Tag, "version", selection.Version, "digest", selection.Candidate.Digest, "platform", target.Platform())
		current := currentByID[image.ID]
		sourceRef := image.Source + ":" + selection.Candidate.Tag
		currentDigest := ""
		bumpedVersion := ""
		plannedVersion := selection.Version
		plannedDigestChanged := false
		if mutable {
			bumpedVersion, err = versioning.BumpPatch(request.Project.Version)
			if err != nil {
				return Result{}, fmt.Errorf("validate mutable image version for %q: %w", image.ID, err)
			}
			currentDigest = referenceDigest(current.UpstreamRef)
			if request.OfficialReviewVersion != "" {
				switch {
				case currentDigest != "":
					plannedDigestChanged = !strings.EqualFold(strings.TrimSpace(currentDigest), strings.TrimSpace(selection.Candidate.Digest))
					plannedVersion = request.Project.Version
					if plannedDigestChanged {
						plannedVersion = bumpedVersion
					}
				case image.Delivery.Mode == "lazycat" && !strings.HasPrefix(strings.TrimSpace(current.RuntimeRef), "registry.lazycat.cloud/"):
					plannedVersion = request.Project.Version
				default:
					return Result{}, fmt.Errorf("compare official review for mutable image %q: trusted source digest baseline is missing", image.ID)
				}
			}
		}
		if request.OfficialReviewVersion != "" && image.ID == request.Config.Update.VersionSource.Image {
			reviewVersion, reviewErr := semver.StrictNewVersion(strings.TrimSpace(request.OfficialReviewVersion))
			candidateVersion, candidateErr := semver.StrictNewVersion(strings.TrimSpace(plannedVersion))
			if reviewErr != nil || candidateErr != nil {
				return Result{}, fmt.Errorf("compare official review version %q with selected candidate version %q: %w", request.OfficialReviewVersion, plannedVersion, errors.Join(reviewErr, candidateErr))
			}
			if !reviewVersion.LessThan(candidateVersion) {
				return Result{}, fmt.Errorf("%w: review %s is not older than candidate %s", ErrOfficialReviewCoversCandidate, reviewVersion, candidateVersion)
			}
			logger.Info("selected image version is newer than official review; automatic publication continues", "image_id", image.ID, "review_version", reviewVersion, "candidate_version", candidateVersion)
		}
		if !mutable && request.Config.Update.VersionSource.Type == config.VersionSourceImage && request.Config.Update.VersionSource.Image == image.ID && !request.Config.Update.AllowDowngrade {
			currentVersion, currentErr := semver.StrictNewVersion(strings.TrimSpace(request.Project.Version))
			selectedVersion, selectedErr := semver.StrictNewVersion(selection.Version)
			if currentErr != nil || selectedErr != nil {
				return Result{}, fmt.Errorf("compare selected image version %q with current application version %q: %w", selection.Version, request.Project.Version, errors.Join(currentErr, selectedErr))
			}
			if selectedVersion.LessThan(currentVersion) {
				return Result{}, fmt.Errorf("%w for %q: selected %s is lower than current %s", ErrVersionDowngrade, image.ID, selection.Version, request.Project.Version)
			}
		}
		deliveryRequest := delivery.Request{
			Image: image, Tag: selection.Candidate.Tag, SourceRef: sourceRef, SourceDigest: selection.Candidate.Digest,
			CurrentRef: current.RuntimeRef, CurrentDigest: currentDigest,
			Mutable: mutable, Target: target, DryRun: request.DryRun,
		}
		if request.OnProgress != nil {
			deliveryRequest.OnProgress = func(progress appstore.CopyProgress) { request.OnProgress(image.ID, progress) }
		}
		var progressMu sync.Mutex
		layerProgress := map[string]int{}
		previousProgress := deliveryRequest.OnProgress
		deliveryRequest.OnProgress = func(progress appstore.CopyProgress) {
			if previousProgress != nil {
				previousProgress(progress)
			}
			progressMu.Lock()
			defer progressMu.Unlock()
			for _, layer := range progress.Layers {
				last := layerProgress[layer.Hash]
				if layer.Progress == 100 || layer.Progress >= last+25 {
					layerProgress[layer.Hash] = layer.Progress
					logger.Info("Docker image layer progress", "image_id", image.ID, "layer", layer.Hash, "progress", layer.Progress)
				}
			}
			if progress.Finished {
				logger.Info("Docker image copy stream completed", "image_id", image.ID)
			}
		}
		logDeliveryStarted(logger, image, sourceRef)

		needsUpdate := false
		var delivered delivery.Result
		if mutable {
			delivered, err = flow.Deliverer.Deliver(ctx, deliveryRequest)
			if err != nil {
				return Result{}, wrapDeliveryError(image, err)
			}
			if request.OfficialReviewVersion != "" && delivered.DigestChanged != plannedDigestChanged {
				return Result{}, fmt.Errorf("mutable image %q digest plan changed during official review gate", image.ID)
			}
			needsUpdate = delivered.DigestChanged || delivered.DeliveryChanged
		} else {
			switch image.Delivery.Mode {
			case "direct", "mirror":
				delivered, err = flow.Deliverer.Deliver(ctx, deliveryRequest)
				if err != nil {
					return Result{}, wrapDeliveryError(image, err)
				}
				needsUpdate = current.UpstreamRef != sourceRef || current.RuntimeRef != delivered.RuntimeRef
			case "lazycat":
				shouldDeliver := current.UpstreamRef != sourceRef || current.RuntimeRef == "" || !strings.HasPrefix(current.RuntimeRef, "registry.lazycat.cloud/") || image.Sort == "created" || image.Sort == "updated"
				if shouldDeliver {
					delivered, err = flow.Deliverer.Deliver(ctx, deliveryRequest)
					if err != nil {
						return Result{}, wrapDeliveryError(image, err)
					}
				} else {
					delivered = delivery.Result{Mode: "lazycat", RuntimeRef: current.RuntimeRef}
				}
				if request.DryRun {
					needsUpdate = shouldDeliver
				} else {
					needsUpdate = current.UpstreamRef != sourceRef || current.RuntimeRef != delivered.RuntimeRef
				}
			default:
				return Result{}, fmt.Errorf("unsupported delivery mode %q", image.Delivery.Mode)
			}
		}
		if delivered.RuntimeRef == "" {
			delivered.RuntimeRef = current.RuntimeRef
		}
		logDeliveryCompleted(logger, image, delivered)
		if needsUpdate && !request.DryRun && delivered.RuntimeRef == "" {
			return Result{}, fmt.Errorf("%w for %q: delivery returned an empty runtime reference", ErrDeliveryFailed, image.ID)
		}
		if needsUpdate {
			result.Changed = true
			if !request.DryRun {
				manifestSourceRef := sourceRef
				if mutable {
					manifestSourceRef += "@" + selection.Candidate.Digest
				}
				updates = append(updates, manifestedit.Update{Target: imageTarget(image), SourceRef: manifestSourceRef, RuntimeRef: delivered.RuntimeRef})
			}
		}
		selectedVersion := selection.Version
		if mutable {
			selectedVersion = request.Project.Version
			if delivered.DigestChanged {
				selectedVersion = bumpedVersion
			}
			logger.Info("mutable Docker image digest compared", "image_id", image.ID, "current_digest", delivered.CurrentDigest, "source_digest", selection.Candidate.Digest, "changed", delivered.DigestChanged, "previous_version", request.Project.Version, "selected_version", selectedVersion)
		}
		imageResult := ImageResult{
			ID: image.ID, Target: image.Target, Service: image.Service, Platform: target.Platform(),
			Tag: selection.Candidate.Tag, SourceRef: sourceRef, SourceDigest: selection.Candidate.Digest,
			CurrentDigest: delivered.CurrentDigest, DigestChanged: delivered.DigestChanged,
			DeliveryMode: image.Delivery.Mode, DeliveredRef: delivered.RuntimeRef, Copied: delivered.Copied,
			CopyResult: copyResult(delivered.CopyResult),
		}
		if mutable {
			imageResult.Bump = request.Config.Update.VersionSource.Bump
			imageResult.PreviousVersion = request.Project.Version
			imageResult.SelectedVersion = selectedVersion
		}
		result.Images = append(result.Images, imageResult)
		if request.Config.Update.VersionSource.Type == config.VersionSourceImage && request.Config.Update.VersionSource.Image == image.ID {
			result.Version = selectedVersion
			result.Channel = image.Channel
		}
	}
	if len(updates) > 0 {
		changes, err := applyManifest(request.Project.ManifestFile, updates)
		if err != nil {
			return Result{}, fmt.Errorf("apply manifest image updates: %w", err)
		}
		if len(changes) != len(updates) {
			return Result{}, errors.New("manifest editor returned an incomplete change set")
		}
	}
	logger.Info("Docker image update completed", "changed", result.Changed, "version", result.Version, "images", len(result.Images))
	return result, nil
}

func prioritizeImage(images []config.Image, id string) []config.Image {
	for index := range images {
		if images[index].ID == id {
			if index == 0 {
				return images
			}
			prioritized := make([]config.Image, 0, len(images))
			prioritized = append(prioritized, images[index])
			prioritized = append(prioritized, images[:index]...)
			prioritized = append(prioritized, images[index+1:]...)
			return prioritized
		}
	}
	return images
}

func wrapDeliveryError(image config.Image, err error) error {
	if errors.Is(err, delivery.ErrMirrorVerification) {
		return fmt.Errorf("%w for %q: %w", ErrMirrorVerificationFailed, image.ID, err)
	}
	if image.Delivery.Mode == "mirror" {
		return fmt.Errorf("prepare mirror image %q: %w", image.ID, err)
	}
	return fmt.Errorf("%w for %q: %w", ErrDeliveryFailed, image.ID, err)
}

func logDeliveryStarted(logger *slog.Logger, image config.Image, sourceRef string) {
	if image.Delivery.Mode == "mirror" {
		logger.Info("Docker mirror verification started", "image_id", image.ID, "source", sourceRef)
		return
	}
	logger.Info("Docker image delivery started", "image_id", image.ID, "mode", image.Delivery.Mode, "source", sourceRef)
}

func logDeliveryCompleted(logger *slog.Logger, image config.Image, delivered delivery.Result) {
	if image.Delivery.Mode == "mirror" {
		logger.Info("Docker mirror verification completed", "image_id", image.ID, "runtime_ref", delivered.RuntimeRef)
		return
	}
	logger.Info("Docker image delivery completed", "image_id", image.ID, "mode", image.Delivery.Mode, "copied", delivered.Copied, "runtime_ref", delivered.RuntimeRef)
}

func referenceDigest(reference string) string {
	_, digest, found := strings.Cut(strings.TrimSpace(reference), "@")
	if !found {
		return ""
	}
	return strings.TrimSpace(digest)
}

func selectImages(images []config.Image, imageID string) ([]config.Image, error) {
	if len(images) == 0 {
		return nil, errors.New("no images are configured")
	}
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return append([]config.Image(nil), images...), nil
	}
	for _, image := range images {
		if image.ID == imageID {
			return []config.Image{image}, nil
		}
	}
	return nil, fmt.Errorf("image ID %q is not configured", imageID)
}

func imageTarget(image config.Image) manifestedit.Target {
	return manifestedit.Target{ID: image.ID, Kind: manifestedit.TargetKind(image.Target), Service: image.Service}
}

func imageRule(image config.Image) (versioning.Rule, registry.TagFilter, error) {
	compile := func(label, expression string) (*regexp.Regexp, error) {
		if strings.TrimSpace(expression) == "" {
			return nil, nil
		}
		compiled, err := regexp.Compile(expression)
		if err != nil {
			return nil, fmt.Errorf("compile image %q %s: %w", image.ID, label, err)
		}
		return compiled, nil
	}
	tagRegex, err := compile("tag_regex", image.TagRegex)
	if err != nil {
		return versioning.Rule{}, registry.TagFilter{}, err
	}
	excludeRegex, err := compile("exclude_regex", image.ExcludeRegex)
	if err != nil {
		return versioning.Rule{}, registry.TagFilter{}, err
	}
	versionRegex, err := compile("version_regex", image.VersionRegex)
	if err != nil {
		return versioning.Rule{}, registry.TagFilter{}, err
	}
	rule := versioning.Rule{
		Channel: versioning.Channel(image.Channel), Sort: versioning.Sort(image.Sort), TagRegex: tagRegex,
		ExcludeRegex: excludeRegex, VersionRegex: versionRegex, VersionTemplate: image.VersionTemplate,
	}
	filter := registry.TagFilter{
		Include: tagRegex, Exclude: excludeRegex,
		MaxTags: image.MaxTags, MaxMatchingTags: image.MaxMatchingTags,
	}
	if rule.Sort == versioning.SortSemVer {
		filter.SemVerRule = &rule
	}
	if rule.Sort == versioning.SortUpdated {
		filter.UpdatedRule = &rule
	}
	return rule, filter, nil
}

func copyResult(value *appstore.CopyImageResult) *CopyResult {
	if value == nil {
		return nil
	}
	layers := make([]LayerProgress, 0, len(value.Progress.Layers))
	for _, layer := range value.Progress.Layers {
		layers = append(layers, LayerProgress{Hash: layer.Hash, Progress: layer.Progress})
	}
	return &CopyResult{
		SourceImage: value.SourceImage, Platform: value.Platform, LazyCatImage: value.LazyCatImage,
		Finished: value.Progress.Finished, Layers: layers,
	}
}
