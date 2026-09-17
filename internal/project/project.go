package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/manifesttemplate"
	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

type Kind string

const (
	KindStatic  Kind = "static"
	KindExec    Kind = "exec"
	KindService Kind = "service"
)

type Info struct {
	Root         string `json:"root"`
	BuildConfig  string `json:"buildConfig"`
	PackageFile  string `json:"packageFile"`
	ManifestFile string `json:"manifestFile"`
	Output       string `json:"output"`
	PackageID    string `json:"packageId"`
	Version      string `json:"version"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Kind         Kind   `json:"kind"`
}

func Inspect(ctx context.Context, cfg config.Project) (Info, error) {
	if ctx == nil {
		return Info{}, errors.New("inspect project: nil context")
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return Info{}, fmt.Errorf("inspect project root: %w", err)
	}
	root = filepath.Clean(root)
	rootInfo, err := os.Stat(root)
	if err != nil {
		return Info{}, fmt.Errorf("inspect project root %q: %w", root, err)
	}
	if !rootInfo.IsDir() {
		return Info{}, fmt.Errorf("inspect project root %q: not a directory", root)
	}

	buildConfig, err := beneath(root, cfg.BuildConfig)
	if err != nil {
		return Info{}, fmt.Errorf("inspect build config: %w", err)
	}
	if err := rejectSymlinkComponents(root, buildConfig); err != nil {
		return Info{}, fmt.Errorf("inspect build config: %w", err)
	}
	loaded, err := toolkitbuild.LoadConfig(ctx, root, buildConfig, map[string]string{})
	if err != nil {
		return Info{}, fmt.Errorf("inspect build config: %w", err)
	}
	manifestName := strings.TrimSpace(loaded.Config.Manifest)
	if manifestName == "" {
		manifestName = "lzc-manifest.yml"
	}
	manifestFile, err := beneath(root, manifestName)
	if err != nil {
		return Info{}, fmt.Errorf("inspect manifest: %w", err)
	}
	packageFile, err := beneath(root, cfg.PackageFile)
	if err != nil {
		return Info{}, fmt.Errorf("inspect package: %w", err)
	}
	output, err := beneath(root, cfg.Output)
	if err != nil {
		return Info{}, fmt.Errorf("inspect output: %w", err)
	}
	for label, name := range map[string]string{"manifest": manifestFile, "package": packageFile, "output": output} {
		if err := rejectSymlinkComponents(root, name); err != nil {
			return Info{}, fmt.Errorf("inspect %s: %w", label, err)
		}
	}

	packageData, err := os.ReadFile(packageFile)
	if err != nil {
		return Info{}, fmt.Errorf("inspect package %q: %w", packageFile, err)
	}
	packageDocument, err := manifest.Parse(packageData)
	if err != nil {
		return Info{}, fmt.Errorf("inspect package %q: %w", packageFile, err)
	}
	var packageInfo manifest.PackageInfo
	if err := packageDocument.Decode(&packageInfo); err != nil {
		return Info{}, fmt.Errorf("inspect package %q: %w", packageFile, err)
	}
	if strings.TrimSpace(packageInfo.Package) == "" || strings.TrimSpace(packageInfo.Version) == "" {
		return Info{}, fmt.Errorf("inspect package %q: package and version are required", packageFile)
	}

	manifestData, err := os.ReadFile(manifestFile)
	if err != nil {
		return Info{}, fmt.Errorf("inspect manifest %q: %w", manifestFile, err)
	}
	protectedManifest, err := manifesttemplate.Protect(manifestData)
	if err != nil {
		return Info{}, fmt.Errorf("inspect manifest %q: %w", manifestFile, err)
	}
	manifestDocument, err := manifest.Parse(protectedManifest.Bytes())
	if err != nil {
		return Info{}, fmt.Errorf("inspect manifest %q: %w", manifestFile, err)
	}
	var typed manifest.Manifest
	if err := manifestDocument.Decode(&typed); err != nil {
		return Info{}, fmt.Errorf("inspect manifest %q: %w", manifestFile, err)
	}

	kind := KindStatic
	if len(typed.Services) > 0 {
		kind = KindService
	} else if routes, found, lookupErr := manifestDocument.Lookup("application", "routes"); lookupErr != nil {
		return Info{}, fmt.Errorf("inspect application routes: %w", lookupErr)
	} else if found && containsExecRoute(routes) {
		kind = KindExec
	} else if upstreams, found, lookupErr := manifestDocument.Lookup("application", "upstreams"); lookupErr != nil {
		return Info{}, fmt.Errorf("inspect application upstreams: %w", lookupErr)
	} else if found && containsLaunchCommand(upstreams) {
		kind = KindExec
	}

	return Info{
		Root:         root,
		BuildConfig:  loaded.Path,
		PackageFile:  packageFile,
		ManifestFile: manifestFile,
		Output:       output,
		PackageID:    strings.TrimSpace(packageInfo.Package),
		Version:      strings.TrimSpace(packageInfo.Version),
		Name:         strings.TrimSpace(packageInfo.Name),
		Description:  strings.TrimSpace(packageInfo.Description),
		Kind:         kind,
	}, nil
}

func beneath(root, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("path %q must be relative to project root", name)
	}
	joined := filepath.Clean(filepath.Join(root, name))
	relative, err := filepath.Rel(root, joined)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes project root", name)
	}
	return joined, nil
}

func rejectSymlinkComponents(root, name string) error {
	relative, err := filepath.Rel(root, name)
	if err != nil {
		return err
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path %q contains a symbolic link", name)
		}
	}
	return nil
}

func containsExecRoute(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, "exec://")
	case []any:
		for _, item := range typed {
			if containsExecRoute(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if containsExecRoute(key) || containsExecRoute(item) {
				return true
			}
		}
	case map[any]any:
		for key, item := range typed {
			if containsExecRoute(key) || containsExecRoute(item) {
				return true
			}
		}
	}
	return false
}

func containsLaunchCommand(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if containsLaunchCommand(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if key == "backend_launch_command" && strings.TrimSpace(fmt.Sprint(item)) != "" {
				return true
			}
			if containsLaunchCommand(item) {
				return true
			}
		}
	case map[any]any:
		for key, item := range typed {
			if strings.TrimSpace(fmt.Sprint(key)) == "backend_launch_command" && strings.TrimSpace(fmt.Sprint(item)) != "" {
				return true
			}
			if containsLaunchCommand(item) {
				return true
			}
		}
	}
	return false
}
