package yamledit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wcaqrl/lazycat-action/internal/appversion"
	"go.yaml.in/yaml/v3"
)

type Change struct {
	Changed bool   `json:"changed"`
	Old     string `json:"old"`
	New     string `json:"new"`
}

func IsValidPackageVersion(value string) bool {
	return appversion.IsValid(value)
}

func SetPackageVersion(filename, value string) (Change, error) {
	if !IsValidPackageVersion(value) {
		return Change{}, fmt.Errorf("invalid package version %q: expected SemVer without a leading v", value)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return Change{}, fmt.Errorf("read package file %q: %w", filename, err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		return Change{}, fmt.Errorf("stat package file %q: %w", filename, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Change{}, fmt.Errorf("parse package file %q: %w", filename, err)
	}
	mapping, err := rootMapping(&document)
	if err != nil {
		return Change{}, fmt.Errorf("parse package file %q: %w", filename, err)
	}

	old, versionNode, versionIndex := findMappingValue(mapping, "version")
	if versionNode != nil && old == value {
		return Change{Changed: false, Old: old, New: value}, nil
	}
	if versionNode == nil {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "version"}
		versionNode = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
		_, _, packageIndex := findMappingValue(mapping, "package")
		insertAt := len(mapping.Content)
		if packageIndex >= 0 {
			insertAt = packageIndex + 2
		}
		mapping.Content = insertNodes(mapping.Content, insertAt, key, versionNode)
	} else {
		_ = versionIndex
		versionNode.Kind = yaml.ScalarNode
		versionNode.Tag = "!!str"
		versionNode.Value = value
		versionNode.Style = 0
	}

	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	encodeErr := encoder.Encode(&document)
	closeErr := encoder.Close()
	if encodeErr != nil || closeErr != nil {
		return Change{}, fmt.Errorf("encode package file %q: %w", filename, errors.Join(encodeErr, closeErr))
	}
	if err := atomicReplace(filename, encoded.Bytes(), info.Mode().Perm()); err != nil {
		return Change{}, err
	}
	return Change{Changed: true, Old: old, New: value}, nil
}

func rootMapping(document *yaml.Node) (*yaml.Node, error) {
	if document == nil || document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("expected one YAML document")
	}
	mapping := document.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil, errors.New("expected a YAML mapping")
	}
	return mapping, nil
}

func findMappingValue(mapping *yaml.Node, wanted string) (string, *yaml.Node, int) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == wanted {
			return mapping.Content[index+1].Value, mapping.Content[index+1], index
		}
	}
	return "", nil, -1
}

func insertNodes(nodes []*yaml.Node, index int, values ...*yaml.Node) []*yaml.Node {
	nodes = append(nodes, make([]*yaml.Node, len(values))...)
	copy(nodes[index+len(values):], nodes[index:len(nodes)-len(values)])
	copy(nodes[index:index+len(values)], values)
	return nodes
}

func atomicReplace(filename string, data []byte, mode os.FileMode) (resultErr error) {
	directory := filepath.Dir(filename)
	temporary, err := os.CreateTemp(directory, ".package-*.yml")
	if err != nil {
		return fmt.Errorf("create temporary package file for %q: %w", filename, err)
	}
	temporaryName := temporary.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary package mode for %q: %w", filename, err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary package file for %q: %w", filename, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary package file for %q: %w", filename, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary package file for %q: %w", filename, err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace package file %q: %w", filename, err)
	}
	return nil
}
