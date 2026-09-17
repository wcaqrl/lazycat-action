package lpkcheck

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

type Request struct {
	ProjectRoot       string
	Path              string
	ExpectedPackageID string
	ExpectedVersion   string
	Target            platform.Target
}

type Result struct {
	Path           string `json:"path"`
	PackageID      string `json:"packageId"`
	Version        string `json:"version"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	TargetPlatform string `json:"targetPlatform"`
}

func File(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("check LPK: context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("check LPK: %w", err)
	}
	target, err := request.Target.Normalize()
	if err != nil {
		return Result{}, fmt.Errorf("check LPK: %w", err)
	}
	root, path, err := resolvePath(request.ProjectRoot, request.Path)
	if err != nil {
		return Result{}, err
	}
	if err := rejectSymlinks(root, path); err != nil {
		return Result{}, err
	}
	reader, err := lpk.OpenFile(ctx, path)
	if err != nil {
		return Result{}, fmt.Errorf("open LPK %q: %w", path, err)
	}
	packageDocument, packageErr := reader.PackageInfo(ctx)
	closeErr := reader.Close()
	if packageErr != nil || closeErr != nil {
		return Result{}, fmt.Errorf("read LPK package metadata: %w", errors.Join(packageErr, closeErr))
	}
	var packageInfo manifest.PackageInfo
	if err := packageDocument.Decode(&packageInfo); err != nil {
		return Result{}, fmt.Errorf("decode LPK package metadata: %w", err)
	}
	packageID := strings.TrimSpace(packageInfo.Package)
	version := strings.TrimSpace(packageInfo.Version)
	if expected := strings.TrimSpace(request.ExpectedPackageID); expected != "" && packageID != expected {
		return Result{}, fmt.Errorf("verify LPK package %q: expected %q", packageID, expected)
	}
	if expected := strings.TrimSpace(request.ExpectedVersion); expected != "" && version != expected {
		return Result{}, fmt.Errorf("verify LPK version %q: expected %q", version, expected)
	}
	digest, size, err := HashFile(ctx, path)
	if err != nil {
		return Result{}, err
	}
	return Result{Path: path, PackageID: packageID, Version: version, SHA256: digest, Size: size, TargetPlatform: target.Platform()}, nil
}

func HashFile(ctx context.Context, filename string) (string, int64, error) {
	if ctx == nil {
		return "", 0, errors.New("hash LPK: context is required")
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, fmt.Errorf("open LPK for hashing: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(hash, contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return "", 0, fmt.Errorf("hash LPK: %w", errors.Join(copyErr, closeErr))
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), written, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

func resolvePath(root, name string) (string, string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(name) == "" {
		return "", "", errors.New("check LPK: project root and path are required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("check LPK project root: %w", err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(absoluteRoot, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("check LPK path: %w", err)
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(absoluteRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("check LPK: path must remain beneath project root")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("check LPK path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", errors.New("check LPK: path must be a regular file")
	}
	return absoluteRoot, path, nil
}

func rejectSymlinks(root, path string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("check LPK project root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("check LPK: project root must not be a symbolic link")
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("check LPK path: %w", err)
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("check LPK path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("check LPK: path must not contain a symbolic link")
		}
	}
	return nil
}
