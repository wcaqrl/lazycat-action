package pipelinestate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/source"
	"go.yaml.in/yaml/v3"
)

const Version = 1

type Lock struct {
	Version     int              `yaml:"version"`
	Source      source.Candidate `yaml:"source"`
	Fingerprint string           `yaml:"fingerprint"`
	Status      string           `yaml:"status"`
	Application Application      `yaml:"application"`
}

type Application struct {
	PackageID string `yaml:"package"`
	Version   string `yaml:"version"`
	LPKSHA256 string `yaml:"lpk_sha256,omitempty"`
}

func Read(filename string) (Lock, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return Lock{}, nil
	}
	if err != nil {
		return Lock{}, fmt.Errorf("stat pipeline state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Lock{}, errors.New("pipeline state must be a regular non-symbolic file")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return Lock{}, fmt.Errorf("read pipeline state: %w", err)
	}
	var lock Lock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return Lock{}, fmt.Errorf("decode pipeline state: %w", err)
	}
	if lock.Version != Version {
		return Lock{}, fmt.Errorf("unsupported pipeline state version %d", lock.Version)
	}
	return lock, nil
}

func Write(filename string, lock Lock) error {
	lock.Version = Version
	data, err := yaml.Marshal(lock)
	if err != nil {
		return fmt.Errorf("encode pipeline state: %w", err)
	}
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create pipeline state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".lazycat-state-*")
	if err != nil {
		return fmt.Errorf("create temporary pipeline state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary pipeline state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary pipeline state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary pipeline state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary pipeline state: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace pipeline state: %w", err)
	}
	return nil
}

func Fingerprint(ctx context.Context, cfg config.Config, candidate source.Candidate) (string, error) {
	if ctx == nil {
		return "", errors.New("fingerprint context is required")
	}
	hash := sha256.New()
	writePart(hash, candidate.Kind)
	writePart(hash, candidate.Ref)
	writePart(hash, candidate.Revision)
	writePart(hash, cfg.Build.Prepare.Mode)
	writePart(hash, cfg.Build.Prepare.Command)
	writePart(hash, cfg.Build.Prepare.Dockerfile)
	writePart(hash, cfg.Build.Prepare.Output)
	keys := make([]string, 0, len(cfg.Build.Prepare.BuildArgs))
	for key := range cfg.Build.Prepare.BuildArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writePart(hash, key)
		writePart(hash, cfg.Build.Prepare.BuildArgs[key])
	}
	root, err := filepath.Abs(cfg.Project.Root)
	if err != nil {
		return "", err
	}
	if dockerfile := strings.TrimSpace(cfg.Build.Prepare.Dockerfile); dockerfile != "" {
		path := filepath.Clean(filepath.Join(root, dockerfile))
		if err := hashFile(hash, root, path); err != nil {
			return "", fmt.Errorf("hash build prepare Dockerfile: %w", err)
		}
	}
	contextPath := strings.TrimSpace(cfg.Build.Prepare.Context)
	if contextPath != "" && contextPath != "source" {
		path := filepath.Clean(filepath.Join(root, contextPath))
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", errors.New("build prepare context escapes project root")
		}
		if err := hashTree(ctx, hash, path); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func hashFile(destination io.Writer, root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("file escapes project root")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("file must be a regular non-symbolic file")
	}
	writePart(destination, filepath.ToSlash(relative))
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, file)
	closeErr := file.Close()
	return errors.Join(copyErr, closeErr)
}

func hashTree(ctx context.Context, destination io.Writer, root string) error {
	entries := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "dist" || entry.Name() == "lpk") {
			if path == root {
				return nil
			}
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("build prepare context contains symbolic link %q", path)
		}
		if entry.Type().IsRegular() {
			entries = append(entries, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan build prepare context: %w", err)
	}
	sort.Strings(entries)
	for _, path := range entries {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		writePart(destination, filepath.ToSlash(relative))
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open build prepare input %q: %w", path, err)
		}
		_, copyErr := io.Copy(destination, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return fmt.Errorf("hash build prepare input %q: %w", path, errors.Join(copyErr, closeErr))
		}
	}
	return nil
}

func writePart(writer io.Writer, value string) {
	_, _ = io.WriteString(writer, fmt.Sprintf("%d:%s\n", len(value), value))
}
