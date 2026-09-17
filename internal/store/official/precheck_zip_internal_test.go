package official

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestPreflightOfficialArchiveSkipsNonZIP(t *testing.T) {
	tests := map[string][]byte{
		"opaque data": []byte("not a ZIP archive"),
		"TAR":         testTARArchive(t),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "application.lpk")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := preflightOfficialArchive(context.Background(), path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPreflightOfficialZIPRejectsEntryCountAboveInjectedLimit(t *testing.T) {
	data := testZIPDirectory(t, "manifest.yml", "package.yml")

	if err := preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 2); err != nil {
		t.Fatal(err)
	}
	err := preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 1)
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeInvalidManifest {
		t.Fatalf("err=%v", err)
	}
}

func TestPreflightOfficialZIPRejectsHiddenEntriesBeyondDeclaredCount(t *testing.T) {
	data := testZIPDirectory(t, "manifest.yml", "package.yml")
	eocd := bytes.LastIndex(data, []byte{'P', 'K', 0x05, 0x06})
	if eocd < 0 {
		t.Fatal("ZIP EOCD not found")
	}
	binary.LittleEndian.PutUint16(data[eocd+8:eocd+10], 1)
	binary.LittleEndian.PutUint16(data[eocd+10:eocd+12], 1)

	err := preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 1)
	assertOfficialZIPPreflightError(t, err)
}

func TestPreflightOfficialZIPRejectsDirectoryGapBeforeEOCD(t *testing.T) {
	data := testZIPDirectory(t, "manifest.yml")
	eocd := bytes.LastIndex(data, []byte{'P', 'K', 0x05, 0x06})
	if eocd < 0 {
		t.Fatal("ZIP EOCD not found")
	}
	data = append(data[:eocd], append([]byte{0}, data[eocd:]...)...)

	err := preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 1)
	assertOfficialZIPPreflightError(t, err)
}

func TestPreflightOfficialZIPParsesZIP64EntryCount(t *testing.T) {
	data := minimalZIP64Directory(t, 2)
	if err := preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 2); err != nil {
		t.Fatal(err)
	}
	err := preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 1)
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeInvalidManifest {
		t.Fatalf("err=%v", err)
	}
}

func TestPreflightOfficialZIPRejectsZIP64RecordGapBeforeLocator(t *testing.T) {
	data := minimalZIP64Directory(t, 0)
	data = append(data[:56], append([]byte{0}, data[56:]...)...)

	err := preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 0)
	assertOfficialZIPPreflightError(t, err)
}

func TestPreflightOfficialZIPRejectsMalformedMetadata(t *testing.T) {
	tests := map[string]func([]byte) []byte{
		"EOCD disk": func(data []byte) []byte {
			eocd := len(data) - zipEOCDBytes
			binary.LittleEndian.PutUint16(data[eocd+4:eocd+6], 1)
			return data
		},
		"EOCD directory range": func(data []byte) []byte {
			eocd := len(data) - zipEOCDBytes
			binary.LittleEndian.PutUint32(data[eocd+12:eocd+16], uint32(eocd+1))
			return data
		},
		"ZIP64 locator signature": func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[56:60], 0)
			return data
		},
		"ZIP64 record range": func(data []byte) []byte {
			binary.LittleEndian.PutUint64(data[64:72], uint64(len(data)))
			return data
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			data := mutate(minimalZIP64Directory(t, 0))
			assertOfficialZIPPreflightError(t, preflightOfficialZIP(bytes.NewReader(data), int64(len(data)), 0))
		})
	}
}

func testZIPDirectory(t *testing.T, names ...string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func testTARArchive(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	contents := []byte("fixture")
	if err := writer.WriteHeader(&tar.Header{Name: "manifest.yml", Mode: 0o600, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func assertOfficialZIPPreflightError(t *testing.T, err error) {
	t.Helper()
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeInvalidManifest {
		t.Fatalf("err=%v", err)
	}
}

func minimalZIP64Directory(t *testing.T, entries uint64) []byte {
	t.Helper()
	if entries > 100 {
		t.Fatal("test ZIP64 entry count is unexpectedly large")
	}
	directoryBytes := int(entries) * zipDirectoryHeaderBytes
	recordOffset := directoryBytes
	data := make([]byte, directoryBytes+56+20+22)
	for offset := 0; offset < directoryBytes; offset += zipDirectoryHeaderBytes {
		binary.LittleEndian.PutUint32(data[offset:offset+4], zipDirectorySignature)
	}
	record := data[recordOffset : recordOffset+56]
	binary.LittleEndian.PutUint32(record[0:4], zip64EOCDSignature)
	binary.LittleEndian.PutUint64(record[4:12], zip64EOCDBaseRecordBytes)
	binary.LittleEndian.PutUint32(record[16:20], 0)
	binary.LittleEndian.PutUint32(record[20:24], 0)
	binary.LittleEndian.PutUint64(record[24:32], entries)
	binary.LittleEndian.PutUint64(record[32:40], entries)
	binary.LittleEndian.PutUint64(record[40:48], uint64(directoryBytes))
	binary.LittleEndian.PutUint64(record[48:56], 0)

	locator := data[recordOffset+56 : recordOffset+76]
	binary.LittleEndian.PutUint32(locator[0:4], 0x07064b50)
	binary.LittleEndian.PutUint32(locator[4:8], 0)
	binary.LittleEndian.PutUint64(locator[8:16], uint64(recordOffset))
	binary.LittleEndian.PutUint32(locator[16:20], 1)

	eocd := data[recordOffset+76:]
	binary.LittleEndian.PutUint32(eocd[0:4], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[4:6], 0)
	binary.LittleEndian.PutUint16(eocd[6:8], 0)
	binary.LittleEndian.PutUint16(eocd[8:10], 0xffff)
	binary.LittleEndian.PutUint16(eocd[10:12], 0xffff)
	binary.LittleEndian.PutUint32(eocd[12:16], 0xffffffff)
	binary.LittleEndian.PutUint32(eocd[16:20], 0xffffffff)
	return data
}
