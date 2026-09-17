package manifestedit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/manifesttemplate"
	"go.yaml.in/yaml/v3"
)

type TargetKind string

const (
	TargetService     TargetKind = "service"
	TargetApplication TargetKind = "application"
)

type Target struct {
	ID      string     `json:"id"`
	Kind    TargetKind `json:"kind"`
	Service string     `json:"service,omitempty"`
}

type Current struct {
	ID          string `json:"id"`
	RuntimeRef  string `json:"runtimeRef"`
	UpstreamRef string `json:"upstreamRef"`
}

type Update struct {
	Target     Target
	SourceRef  string
	RuntimeRef string
}

type Change struct {
	ID             string `json:"id"`
	Changed        bool   `json:"changed"`
	OldRuntimeRef  string `json:"oldRuntimeRef"`
	NewRuntimeRef  string `json:"newRuntimeRef"`
	OldUpstreamRef string `json:"oldUpstreamRef"`
	NewUpstreamRef string `json:"newUpstreamRef"`
}

type document struct {
	root     yaml.Node
	mapping  *yaml.Node
	mode     os.FileMode
	template manifesttemplate.Protected
}

type resolved struct {
	update Update
	parent *yaml.Node
	key    *yaml.Node
	image  *yaml.Node
}

func Read(filename string, targets []Target) ([]Current, error) {
	document, err := load(filename)
	if err != nil {
		return nil, err
	}
	if err := validateTargets(targets); err != nil {
		return nil, err
	}
	result := make([]Current, 0, len(targets))
	for _, target := range targets {
		_, key, image, err := resolveTarget(document.mapping, target)
		if err != nil {
			return nil, err
		}
		current := Current{ID: target.ID}
		if image != nil {
			current.RuntimeRef = strings.TrimSpace(image.Value)
			current.UpstreamRef = imageUpstream(key, image)
		}
		result = append(result, current)
	}
	return result, nil
}

func Apply(filename string, updates []Update) ([]Change, error) {
	if len(updates) == 0 {
		return nil, nil
	}
	targets := make([]Target, 0, len(updates))
	for _, update := range updates {
		if strings.TrimSpace(update.SourceRef) == "" || strings.TrimSpace(update.RuntimeRef) == "" {
			return nil, fmt.Errorf("image %q source and runtime references are required", update.Target.ID)
		}
		targets = append(targets, update.Target)
	}
	if err := validateTargets(targets); err != nil {
		return nil, err
	}
	document, err := load(filename)
	if err != nil {
		return nil, err
	}

	resolvedTargets := make([]resolved, 0, len(updates))
	changes := make([]Change, 0, len(updates))
	changed := false
	for _, update := range updates {
		parent, key, image, err := resolveTarget(document.mapping, update.Target)
		if err != nil {
			return nil, err
		}
		current := Current{ID: update.Target.ID}
		if image != nil {
			current.RuntimeRef = strings.TrimSpace(image.Value)
			current.UpstreamRef = imageUpstream(key, image)
		}
		change := Change{
			ID:             update.Target.ID,
			Changed:        current.RuntimeRef != update.RuntimeRef || current.UpstreamRef != update.SourceRef,
			OldRuntimeRef:  current.RuntimeRef,
			NewRuntimeRef:  update.RuntimeRef,
			OldUpstreamRef: current.UpstreamRef,
			NewUpstreamRef: update.SourceRef,
		}
		changed = changed || change.Changed
		changes = append(changes, change)
		resolvedTargets = append(resolvedTargets, resolved{update: update, parent: parent, key: key, image: image})
	}
	if !changed {
		return changes, nil
	}

	for _, target := range resolvedTargets {
		key := target.key
		image := target.image
		if image == nil {
			key = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "image"}
			image = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str"}
			target.parent.Content = append(target.parent.Content, key, image)
		}
		image.Kind = yaml.ScalarNode
		image.Tag = "!!str"
		image.Style = 0
		image.Value = strings.TrimSpace(target.update.RuntimeRef)
		commentNode := key
		if strings.TrimSpace(key.HeadComment) == "" && strings.TrimSpace(image.HeadComment) != "" {
			commentNode = image
		}
		commentNode.HeadComment = setUpstream(commentNode.HeadComment, strings.TrimSpace(target.update.SourceRef))
	}

	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	encodeErr := encoder.Encode(&document.root)
	closeErr := encoder.Close()
	if encodeErr != nil || closeErr != nil {
		return nil, fmt.Errorf("encode manifest %q: %w", filename, errors.Join(encodeErr, closeErr))
	}
	restored, err := document.template.Restore(encoded.Bytes())
	if err != nil {
		return nil, fmt.Errorf("restore manifest %q: %w", filename, err)
	}
	if err := atomicReplace(filename, restored, document.mode); err != nil {
		return nil, err
	}
	return changes, nil
}

func load(filename string) (document, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return document{}, fmt.Errorf("stat manifest %q: %w", filename, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return document{}, fmt.Errorf("manifest %q must not be a symbolic link", filename)
	}
	if !info.Mode().IsRegular() {
		return document{}, fmt.Errorf("manifest %q is not a regular file", filename)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return document{}, fmt.Errorf("read manifest %q: %w", filename, err)
	}
	protected, err := manifesttemplate.Protect(data)
	if err != nil {
		return document{}, fmt.Errorf("parse manifest %q: %w", filename, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(protected.Bytes(), &root); err != nil {
		return document{}, fmt.Errorf("parse manifest %q: %w", filename, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return document{}, fmt.Errorf("parse manifest %q: expected a root mapping", filename)
	}
	return document{root: root, mapping: root.Content[0], mode: info.Mode().Perm(), template: protected}, nil
}

func validateTargets(targets []Target) error {
	ids := make(map[string]struct{}, len(targets))
	paths := make(map[string]string, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.ID) == "" {
			return errors.New("image target ID is required")
		}
		if _, exists := ids[target.ID]; exists {
			return fmt.Errorf("duplicate image target ID %q", target.ID)
		}
		ids[target.ID] = struct{}{}
		key := string(target.Kind)
		switch target.Kind {
		case TargetService:
			if strings.TrimSpace(target.Service) == "" {
				return fmt.Errorf("image target %q requires a service", target.ID)
			}
			key += ":" + target.Service
		case TargetApplication:
			if strings.TrimSpace(target.Service) != "" {
				return fmt.Errorf("application image target %q must not specify service", target.ID)
			}
		default:
			return fmt.Errorf("image target %q has unsupported kind %q", target.ID, target.Kind)
		}
		if existing, found := paths[key]; found {
			return fmt.Errorf("image targets %q and %q both address %q", existing, target.ID, key)
		}
		paths[key] = target.ID
	}
	return nil
}

func resolveTarget(root *yaml.Node, target Target) (*yaml.Node, *yaml.Node, *yaml.Node, error) {
	var parent *yaml.Node
	switch target.Kind {
	case TargetApplication:
		application, found := mappingValue(root, "application")
		if !found || application.Kind != yaml.MappingNode {
			return nil, nil, nil, errors.New("application mapping is missing")
		}
		parent = application
	case TargetService:
		services, found := mappingValue(root, "services")
		if !found || services.Kind != yaml.MappingNode {
			return nil, nil, nil, fmt.Errorf("service %q is missing", target.Service)
		}
		service, found := mappingValue(services, target.Service)
		if !found || service.Kind != yaml.MappingNode {
			return nil, nil, nil, fmt.Errorf("service %q is missing", target.Service)
		}
		parent = service
	}
	key, image, found := mappingPair(parent, "image")
	if !found {
		return parent, nil, nil, nil
	}
	if image.Kind != yaml.ScalarNode {
		return nil, nil, nil, fmt.Errorf("image target %q is not a scalar", target.ID)
	}
	return parent, key, image, nil
}

func mappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	_, value, found := mappingPair(mapping, key)
	return value, found
}

func mappingPair(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node, bool) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index], mapping.Content[index+1], true
		}
	}
	return nil, nil, false
}

func imageUpstream(key, image *yaml.Node) string {
	if key != nil {
		if upstream := readUpstream(key.HeadComment); upstream != "" {
			return upstream
		}
	}
	if image != nil {
		return readUpstream(image.HeadComment)
	}
	return ""
}

func readUpstream(comment string) string {
	for _, line := range strings.Split(comment, "\n") {
		content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if strings.HasPrefix(content, "upstream:") {
			return strings.TrimSpace(strings.TrimPrefix(content, "upstream:"))
		}
	}
	return ""
}

func setUpstream(comment, source string) string {
	lines := make([]string, 0)
	found := false
	for _, line := range strings.Split(comment, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if strings.HasPrefix(content, "upstream:") {
			if !found {
				lines = append(lines, "# upstream: "+source)
				found = true
			}
			continue
		}
		lines = append(lines, line)
	}
	if !found {
		lines = append(lines, "# upstream: "+source)
	}
	return strings.Join(lines, "\n")
}

func atomicReplace(filename string, data []byte, mode os.FileMode) (resultErr error) {
	directory := filepath.Dir(filename)
	temporary, err := os.CreateTemp(directory, ".manifest-*.yml")
	if err != nil {
		return fmt.Errorf("create temporary manifest for %q: %w", filename, err)
	}
	temporaryName := temporary.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace manifest %q: %w", filename, err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := directoryHandle.Sync()
	closeErr := directoryHandle.Close()
	if syncErr != nil || closeErr != nil {
		return errors.Join(syncErr, closeErr)
	}
	return nil
}
