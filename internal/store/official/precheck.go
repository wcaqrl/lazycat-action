package official

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	toolkitarchive "github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lint"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"go.yaml.in/yaml/v3"
)

const (
	// The official precheck enumerates, but never expands, the complete package.
	// These limits fit normal CI-built application/image packages while bounding
	// archive metadata, decompression claims, and entry-list memory.
	officialPrecheckMaxInputBytes    int64 = 8 << 30
	officialPrecheckMaxEntries             = 10_000
	officialPrecheckMaxFileBytes     int64 = 4 << 30
	officialPrecheckMaxTotalBytes    int64 = 16 << 30
	officialPrecheckMaxPathBytes           = 1024
	officialPrecheckMaxDocumentBytes int64 = 8 << 20
	officialPrecheckIconBytes        int64 = lint.OfficialIconMaxBytes + 1
)

var officialPrecheckArchiveLimits = toolkitarchive.Limits{
	MaxInputBytes:    officialPrecheckMaxInputBytes,
	MaxEntries:       officialPrecheckMaxEntries,
	MaxFileBytes:     officialPrecheckMaxFileBytes,
	MaxTotalBytes:    officialPrecheckMaxTotalBytes,
	MaxPathBytes:     officialPrecheckMaxPathBytes,
	MaxDocumentBytes: officialPrecheckMaxDocumentBytes,
}

type officialPrecheckImagesLock struct {
	Images map[string]struct {
		Layers []struct {
			Digest string `yaml:"digest"`
		} `yaml:"layers"`
	} `yaml:"images"`
}

// PrecheckFile validates the official-store lint profile for a verified LPK
// without acquiring credentials, making network requests, or expanding payloads.
func PrecheckFile(ctx context.Context, lpkPath string) error {
	if ctx == nil {
		return &lpkgo.Error{
			Code:  lpkgo.CodeInvalidArgument,
			Op:    "store.official.precheck",
			Cause: errors.New("official manifest precheck requires a context"),
		}
	}
	if err := preflightOfficialArchive(ctx, lpkPath); err != nil {
		return officialPrecheckError(ctx, err)
	}
	reader, err := lpk.OpenFile(ctx, lpkPath, lpk.WithLimits(officialPrecheckArchiveLimits))
	if err != nil {
		return officialPrecheckError(ctx, err)
	}
	entries, err := reader.Entries(ctx)
	if err != nil {
		return officialPrecheckError(ctx, errors.Join(err, reader.Close()))
	}
	lintParent, err := os.MkdirTemp("", "lazycat-action-official-lint-*")
	if err != nil {
		return officialPrecheckError(ctx, errors.Join(err, reader.Close()))
	}
	defer os.RemoveAll(lintParent)

	lintRoot := filepath.Join(lintParent, "root")
	materializeErr := materializeOfficialLintRoot(ctx, reader, entries, lintRoot)
	closeErr := reader.Close()
	if materializeErr != nil || closeErr != nil {
		return officialPrecheckError(ctx, errors.Join(materializeErr, closeErr))
	}
	warnings, err := lint.Package(ctx, os.DirFS(lintRoot), lint.WithOfficial())
	if err != nil {
		return officialPrecheckError(ctx, err)
	}
	for _, warning := range warnings {
		if lint.IsOfficialWarning(warning) {
			return publishError(lpkgo.CodeInvalidManifest, errors.New("official manifest validation failed"))
		}
	}
	return nil
}

func materializeOfficialLintRoot(ctx context.Context, reader *lpk.Reader, entries []toolkitarchive.Entry, root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	entryByName := make(map[string]toolkitarchive.Entry, len(entries))
	for _, entry := range entries {
		entryByName[entry.Name] = entry
	}
	for _, name := range []string{"manifest.yml", "package.yml"} {
		if err := materializeDocument(ctx, reader, entryByName, root, name); err != nil {
			return err
		}
	}
	if err := materializeIcon(ctx, reader, entryByName, root); err != nil {
		return err
	}
	if entry, ok := entryByName["devshell"]; ok && entry.Type != toolkitarchive.EntryDirectory {
		if err := writeLintFile(root, "devshell", nil); err != nil {
			return err
		}
	}
	imagesLock, err := materializeImagesLock(ctx, reader, entryByName, root)
	if err != nil {
		return err
	}
	if imagesLock != nil {
		if err := materializeReferencedImageBlobs(entryByName, root, imagesLock); err != nil {
			return err
		}
	}
	if _, hasManifest := entryByName["manifest.yml"]; !hasManifest {
		if err := materializeResourceExports(entries, root); err != nil {
			return err
		}
	}
	return nil
}

func materializeDocument(ctx context.Context, reader *lpk.Reader, entries map[string]toolkitarchive.Entry, root, name string) error {
	entry, ok := entries[name]
	if !ok {
		return nil
	}
	if entry.Type != toolkitarchive.EntryRegular {
		return officialPrecheckMetadataError(lpkgo.CodeInvalidManifest)
	}
	data, err := readOfficialPrecheckEntry(ctx, reader, entry, officialPrecheckMaxDocumentBytes)
	if err != nil {
		return err
	}
	return writeLintFile(root, name, data)
}

func materializeIcon(ctx context.Context, reader *lpk.Reader, entries map[string]toolkitarchive.Entry, root string) error {
	entry, ok := entries["icon.png"]
	if !ok || entry.Type != toolkitarchive.EntryRegular {
		return nil
	}
	data, err := readOfficialPrecheckEntryPrefix(ctx, reader, entry, officialPrecheckIconBytes)
	if err != nil {
		return err
	}
	return writeLintFile(root, "icon.png", data)
}

func materializeImagesLock(ctx context.Context, reader *lpk.Reader, entries map[string]toolkitarchive.Entry, root string) (*officialPrecheckImagesLock, error) {
	entry, ok := entries["images.lock"]
	if !ok {
		return nil, nil
	}
	if entry.Type != toolkitarchive.EntryRegular {
		return nil, officialPrecheckMetadataError(lpkgo.CodeInvalidManifest)
	}
	data, err := readOfficialPrecheckEntry(ctx, reader, entry, officialPrecheckMaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	if err := writeLintFile(root, "images.lock", data); err != nil {
		return nil, err
	}
	var imagesLock officialPrecheckImagesLock
	if err := yaml.Unmarshal(data, &imagesLock); err != nil {
		return nil, nil
	}
	return &imagesLock, nil
}

func materializeReferencedImageBlobs(entries map[string]toolkitarchive.Entry, root string, imagesLock *officialPrecheckImagesLock) error {
	for _, image := range imagesLock.Images {
		for _, layer := range image.Layers {
			digest := strings.ToLower(strings.TrimSpace(layer.Digest))
			hex := strings.TrimPrefix(digest, "sha256:")
			if digest == hex || !sha256Pattern.MatchString(hex) {
				continue
			}
			name := "images/blobs/sha256/" + hex
			entry, ok := entries[name]
			if !ok || entry.Type != toolkitarchive.EntryRegular {
				continue
			}
			if err := writeLintFile(root, name, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func materializeResourceExports(entries []toolkitarchive.Entry, root string) error {
	for _, entry := range entries {
		if entry.Name != "exports" && !strings.HasPrefix(entry.Name, "exports/") {
			continue
		}
		if !fs.ValidPath(entry.Name) {
			return officialPrecheckMetadataError(lpkgo.CodeInvalidArgument)
		}
		path := filepath.Join(root, filepath.FromSlash(entry.Name))
		if entry.Type == toolkitarchive.EntryDirectory {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := writeLintFile(root, entry.Name, nil); err != nil {
			return err
		}
	}
	return nil
}

func readOfficialPrecheckEntry(ctx context.Context, reader *lpk.Reader, entry toolkitarchive.Entry, limit int64) ([]byte, error) {
	if limit < 0 || entry.Size > limit || limit == math.MaxInt64 {
		return nil, officialPrecheckMetadataError(lpkgo.CodeInvalidArgument)
	}
	input, err := reader.OpenEntry(ctx, entry.Name)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(input, limit+1))
	closeErr := input.Close()
	if readErr != nil || closeErr != nil {
		return nil, officialPrecheckIntegrityError(errors.Join(readErr, closeErr))
	}
	if int64(len(data)) > limit {
		return nil, officialPrecheckMetadataError(lpkgo.CodeInvalidManifest)
	}
	if int64(len(data)) != entry.Size {
		return nil, officialPrecheckIntegrityError(errors.New("official manifest metadata size does not match archive directory"))
	}
	return data, nil
}

func readOfficialPrecheckEntryPrefix(ctx context.Context, reader *lpk.Reader, entry toolkitarchive.Entry, limit int64) ([]byte, error) {
	input, err := reader.OpenEntry(ctx, entry.Name)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(input, limit))
	closeErr := input.Close()
	if readErr != nil || closeErr != nil {
		return nil, officialPrecheckIntegrityError(errors.Join(readErr, closeErr))
	}
	return data, nil
}

func writeLintFile(root, name string, data []byte) error {
	if !fs.ValidPath(name) {
		return officialPrecheckMetadataError(lpkgo.CodeInvalidArgument)
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func officialPrecheckMetadataError(code lpkgo.Code) error {
	return &lpkgo.Error{Code: code, Op: "store.official.precheck", Cause: errors.New("official manifest metadata is invalid")}
}

func officialPrecheckIntegrityError(cause error) error {
	if cause == nil {
		cause = errors.New("official manifest metadata integrity check failed")
	}
	return &lpkgo.Error{Code: lpkgo.CodeIntegrityMismatch, Op: "store.official.precheck", Cause: cause}
}

func officialPrecheckError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return publishContextError(ctxErr)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return publishContextError(err)
	}
	code := lpkgo.CodeCommandFailed
	var toolkitError *lpkgo.Error
	if errors.As(err, &toolkitError) && toolkitError.Code != "" {
		code = officialPrecheckErrorCode(toolkitError)
	}
	return &lpkgo.Error{
		Code:  code,
		Op:    "store.official.precheck",
		Cause: errors.New("official manifest precheck failed"),
	}
}

func officialPrecheckErrorCode(err *lpkgo.Error) lpkgo.Code {
	if err == nil {
		return lpkgo.CodeCommandFailed
	}
	switch err.Code {
	case lpkgo.CodeInvalidArgument,
		lpkgo.CodeInvalidManifest,
		lpkgo.CodeUnsupportedFormat,
		lpkgo.CodeConflict,
		lpkgo.CodeIntegrityMismatch:
		return lpkgo.CodeInvalidManifest
	case lpkgo.CodeNotFound:
		if err.Op != "archive.open_file" {
			return lpkgo.CodeInvalidManifest
		}
	}
	return err.Code
}
