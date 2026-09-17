package imagemirror

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

const (
	EnvDockerMirror    = "LAZYCAT_DOCKER_MIRROR"
	EnvGHCRMirror      = "LAZYCAT_GHCR_MIRROR"
	EnvRegistryMirrors = "LAZYCAT_REGISTRY_MIRRORS"
)

const (
	dockerRegistry = "docker.io"
	ghcrRegistry   = "ghcr.io"
	defaultDocker  = "docker.1ms.run"
	defaultGHCR    = "ghcr.1ms.run"
)

type mapping struct {
	registry string
	prefix   string
	explicit bool
}

// Resolver resolves an upstream repository to a runtime mirror template and
// recognizes configured or built-in mirror prefixes in an existing Manifest.
type Resolver struct {
	mappings map[string]mapping
	extract  []mapping
}

// FromEnvironment validates mirror overrides at the process boundary. Empty
// values are treated as unset so reusable-workflow inputs can default to "".
func FromEnvironment(getenv func(string) string) (Resolver, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	resolver := Resolver{mappings: map[string]mapping{
		dockerRegistry: {registry: dockerRegistry, prefix: defaultDocker},
		ghcrRegistry:   {registry: ghcrRegistry, prefix: defaultGHCR},
	}}

	custom, err := parseRegistryMappings(getenv(EnvRegistryMirrors))
	if err != nil {
		return Resolver{}, fmt.Errorf("%s: %w", EnvRegistryMirrors, err)
	}
	for registry, prefix := range custom {
		resolver.mappings[registry] = mapping{registry: registry, prefix: prefix, explicit: true}
	}
	for _, override := range []struct {
		environment string
		registry    string
	}{
		{environment: EnvDockerMirror, registry: dockerRegistry},
		{environment: EnvGHCRMirror, registry: ghcrRegistry},
	} {
		value := strings.TrimSpace(getenv(override.environment))
		if value == "" {
			continue
		}
		prefix, err := normalizePrefix(value)
		if err != nil {
			return Resolver{}, fmt.Errorf("%s: %w", override.environment, err)
		}
		resolver.mappings[override.registry] = mapping{registry: override.registry, prefix: prefix, explicit: true}
	}

	// Keep the built-ins recognizable after an override so historical
	// docker.1ms.run/ghcr.1ms.run Manifests remain migratable.
	resolver.extract = append(resolver.extract,
		mapping{registry: dockerRegistry, prefix: defaultDocker},
		mapping{registry: ghcrRegistry, prefix: defaultGHCR},
	)
	for _, candidate := range resolver.mappings {
		duplicate := false
		for _, existing := range resolver.extract {
			if candidate.registry == existing.registry && candidate.prefix == existing.prefix {
				duplicate = true
				break
			}
		}
		if !duplicate {
			resolver.extract = append(resolver.extract, candidate)
		}
	}
	if err := validateExtractionMappings(resolver.extract); err != nil {
		return Resolver{}, err
	}
	sort.Slice(resolver.extract, func(i, j int) bool {
		return len(resolver.extract[i].prefix) > len(resolver.extract[j].prefix)
	})
	return resolver, nil
}

// Template returns the effective image template for source. An explicit
// environment mapping overrides a historical template; otherwise the existing
// template is preserved before built-in defaults are considered.
func (resolver Resolver) Template(source, existing string) (string, error) {
	if strings.TrimSpace(source) == "" && strings.TrimSpace(existing) != "" {
		return strings.TrimSpace(existing), nil
	}
	registry, repository, err := parseSource(source)
	if err != nil {
		return "", err
	}
	mapped, found := resolver.mappings[registry]
	if strings.TrimSpace(existing) != "" && (!found || !mapped.explicit) {
		return strings.TrimSpace(existing), nil
	}
	if !found {
		return "", fmt.Errorf("no mirror is configured for registry %q", registry)
	}
	return mapped.prefix + "/" + repository + ":{tag}", nil
}

// ExtractSource recovers an upstream repository from a known mirror runtime
// reference. It returns found=false when the reference is not a known mirror.
func (resolver Resolver) ExtractSource(runtime string) (string, bool, error) {
	repository, err := name.ParseReference(strings.TrimSpace(runtime), name.WeakValidation)
	if err != nil {
		return "", false, fmt.Errorf("parse runtime image reference: %w", err)
	}
	runtimeRepository := canonicalRepository(repository.Context())
	matches := make(map[string]struct{})
	for _, candidate := range resolver.extract {
		if runtimeRepository == candidate.prefix {
			return "", false, fmt.Errorf("mirror reference %q does not contain an upstream repository", runtime)
		}
		prefix := candidate.prefix + "/"
		if !strings.HasPrefix(runtimeRepository, prefix) {
			continue
		}
		upstreamPath := strings.TrimPrefix(runtimeRepository, prefix)
		if upstreamPath == "" {
			return "", false, fmt.Errorf("mirror reference %q does not contain an upstream repository", runtime)
		}
		matches[candidate.registry+"/"+upstreamPath] = struct{}{}
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	if len(matches) > 1 {
		return "", false, fmt.Errorf("mirror reference %q matches multiple upstream repositories", runtime)
	}
	for source := range matches {
		return source, true, nil
	}
	return "", false, errors.New("mirror source resolution produced an inconsistent result")
}

func parseRegistryMappings(value string) (map[string]string, error) {
	result := make(map[string]string)
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		registryValue, prefixValue, found := strings.Cut(item, "=")
		if !found || strings.TrimSpace(registryValue) == "" || strings.TrimSpace(prefixValue) == "" {
			return nil, fmt.Errorf("mapping %q must use registry=mirror-prefix", item)
		}
		registry, err := normalizeRegistry(registryValue)
		if err != nil {
			return nil, fmt.Errorf("mapping %q registry: %w", item, err)
		}
		if _, exists := result[registry]; exists {
			return nil, fmt.Errorf("duplicate registry %q", registry)
		}
		prefix, err := normalizePrefix(prefixValue)
		if err != nil {
			return nil, fmt.Errorf("mapping %q prefix: %w", item, err)
		}
		result[registry] = prefix
	}
	return result, nil
}

func normalizeRegistry(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "/@?#") || strings.Contains(value, "://") {
		return "", fmt.Errorf("invalid registry %q", value)
	}
	registry, err := name.NewRegistry(value, name.StrictValidation)
	if err != nil {
		return "", fmt.Errorf("invalid registry %q: %w", value, err)
	}
	return canonicalRegistry(registry.Name()), nil
}

func normalizePrefix(value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" || strings.Contains(value, "://") || strings.ContainsAny(value, "@?#") {
		return "", fmt.Errorf("invalid mirror prefix %q", value)
	}
	const probe = "lazycat-mirror-prefix-validation"
	repository, err := name.NewRepository(value+"/"+probe, name.StrictValidation)
	if err != nil {
		return "", fmt.Errorf("invalid mirror prefix %q: %w", value, err)
	}
	normalized := canonicalRepository(repository)
	suffix := "/" + probe
	if !strings.HasSuffix(normalized, suffix) {
		return "", fmt.Errorf("invalid mirror prefix %q", value)
	}
	return strings.TrimSuffix(normalized, suffix), nil
}

func parseSource(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", errors.New("source image repository is required")
	}
	repository, err := name.NewRepository(value, name.WeakValidation)
	if err != nil {
		return "", "", fmt.Errorf("parse source image repository %q: %w", value, err)
	}
	registry := canonicalRegistry(repository.RegistryStr())
	path := repository.RepositoryStr()
	if registry == dockerRegistry && !strings.Contains(path, "/") {
		path = "library/" + path
	}
	return registry, path, nil
}

func canonicalRegistry(value string) string {
	if value == name.DefaultRegistry || value == "index.docker.io" {
		return dockerRegistry
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func canonicalRepository(repository name.Repository) string {
	registry := canonicalRegistry(repository.RegistryStr())
	path := repository.RepositoryStr()
	if registry == dockerRegistry && !strings.Contains(path, "/") {
		path = "library/" + path
	}
	return registry + "/" + path
}

func validateExtractionMappings(mappings []mapping) error {
	for index, left := range mappings {
		for _, right := range mappings[index+1:] {
			if left.prefix == right.prefix && left.registry != right.registry {
				return fmt.Errorf("mirror prefix %q is ambiguous for registries %q and %q", left.prefix, left.registry, right.registry)
			}
		}
	}
	return nil
}
