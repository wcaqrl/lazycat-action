package official

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const (
	zipEOCDSignature         = 0x06054b50
	zip64EOCDSignature       = 0x06064b50
	zip64LocatorSignature    = 0x07064b50
	zipEOCDBytes             = 22
	zipMaxCommentBytes       = 1<<16 - 1
	zip64LocatorBytes        = 20
	zip64EOCDMinimumBytes    = 56
	zip64EOCDBaseRecordBytes = 44
	zip64EOCDMaxRecordBytes  = 1 << 20
	zipDirectoryHeaderBytes  = 46
	zipDirectorySignature    = 0x02014b50
	zipDirectoryDigitalSig   = 0x05054b50
	zipDigitalSigHeaderBytes = 6
)

func preflightOfficialArchive(ctx context.Context, lpkPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(lpkPath)
	if err != nil {
		code := lpkgo.CodeCommandFailed
		if errors.Is(err, fs.ErrNotExist) {
			code = lpkgo.CodeNotFound
		}
		return &lpkgo.Error{Code: code, Op: "archive.open_file", Cause: err}
	}
	info, statErr := file.Stat()
	if statErr != nil {
		return errors.Join(statErr, file.Close())
	}
	if !info.Mode().IsRegular() || info.Size() > officialPrecheckMaxInputBytes {
		return errors.Join(officialZIPPreflightError(), file.Close())
	}
	var signature [2]byte
	read, readErr := file.ReadAt(signature[:], 0)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return errors.Join(readErr, file.Close())
	}
	if read < len(signature) || string(signature[:]) != "PK" {
		return file.Close()
	}
	preflightErr := preflightOfficialZIP(file, info.Size(), officialPrecheckMaxEntries)
	closeErr := file.Close()
	return errors.Join(preflightErr, closeErr)
}

// preflightOfficialZIP bounds central-directory entry counts before the
// toolkit constructs archive/zip's entry slice. It reads the ZIP tail, fixed
// ZIP64 metadata, and fixed-size central-directory headers without allocating
// entry names, comments, or extra fields.
func preflightOfficialZIP(reader io.ReaderAt, size int64, maxEntries int) error {
	if reader == nil || size < zipEOCDBytes || maxEntries < 0 {
		return officialZIPPreflightError()
	}
	tailSize := min(size, int64(zipEOCDBytes+zipMaxCommentBytes))
	tail := make([]byte, tailSize)
	if err := readOfficialZIPAt(reader, tail, size-tailSize); err != nil {
		return officialZIPPreflightError()
	}
	eocdIndex := findOfficialZIPEOCD(tail)
	if eocdIndex < 0 {
		return officialZIPPreflightError()
	}
	eocdOffset := size - tailSize + int64(eocdIndex)
	eocd := tail[eocdIndex : eocdIndex+zipEOCDBytes]
	disk := binary.LittleEndian.Uint16(eocd[4:6])
	directoryDisk := binary.LittleEndian.Uint16(eocd[6:8])
	entriesOnDisk := binary.LittleEndian.Uint16(eocd[8:10])
	totalEntries := binary.LittleEndian.Uint16(eocd[10:12])
	directorySize := binary.LittleEndian.Uint32(eocd[12:16])
	directoryOffset := binary.LittleEndian.Uint32(eocd[16:20])
	if disk != 0 || directoryDisk != 0 {
		return officialZIPPreflightError()
	}
	zip64 := entriesOnDisk == math.MaxUint16 || totalEntries == math.MaxUint16 ||
		directorySize == math.MaxUint32 || directoryOffset == math.MaxUint32
	if zip64 {
		return preflightOfficialZIP64(reader, eocdOffset, maxEntries, entriesOnDisk, totalEntries, directorySize, directoryOffset)
	}
	if entriesOnDisk != totalEntries || uint64(totalEntries) > uint64(maxEntries) {
		return officialZIPPreflightError()
	}
	return validateOfficialZIPDirectory(
		reader,
		uint64(directoryOffset),
		uint64(directorySize),
		uint64(eocdOffset),
		uint64(totalEntries),
		maxEntries,
	)
}

func preflightOfficialZIP64(
	reader io.ReaderAt,
	eocdOffset int64,
	maxEntries int,
	eocdEntriesOnDisk, eocdTotalEntries uint16,
	eocdDirectorySize, eocdDirectoryOffset uint32,
) error {
	locatorOffset := eocdOffset - zip64LocatorBytes
	if locatorOffset < 0 {
		return officialZIPPreflightError()
	}
	locator := make([]byte, zip64LocatorBytes)
	if err := readOfficialZIPAt(reader, locator, locatorOffset); err != nil ||
		binary.LittleEndian.Uint32(locator[0:4]) != zip64LocatorSignature ||
		binary.LittleEndian.Uint32(locator[4:8]) != 0 ||
		binary.LittleEndian.Uint32(locator[16:20]) != 1 {
		return officialZIPPreflightError()
	}
	recordOffset := binary.LittleEndian.Uint64(locator[8:16])
	if recordOffset > math.MaxInt64 || recordOffset+zip64EOCDMinimumBytes > uint64(locatorOffset) {
		return officialZIPPreflightError()
	}
	record := make([]byte, zip64EOCDMinimumBytes)
	if err := readOfficialZIPAt(reader, record, int64(recordOffset)); err != nil ||
		binary.LittleEndian.Uint32(record[0:4]) != zip64EOCDSignature {
		return officialZIPPreflightError()
	}
	recordSize := binary.LittleEndian.Uint64(record[4:12])
	if recordSize < zip64EOCDBaseRecordBytes || recordSize > zip64EOCDMaxRecordBytes ||
		recordOffset > math.MaxUint64-(12+recordSize) || recordOffset+12+recordSize != uint64(locatorOffset) {
		return officialZIPPreflightError()
	}
	if binary.LittleEndian.Uint32(record[16:20]) != 0 || binary.LittleEndian.Uint32(record[20:24]) != 0 {
		return officialZIPPreflightError()
	}
	entriesOnDisk := binary.LittleEndian.Uint64(record[24:32])
	totalEntries := binary.LittleEndian.Uint64(record[32:40])
	directorySize := binary.LittleEndian.Uint64(record[40:48])
	directoryOffset := binary.LittleEndian.Uint64(record[48:56])
	if entriesOnDisk != totalEntries || totalEntries > uint64(maxEntries) ||
		directoryOffset > math.MaxUint64-directorySize || directoryOffset+directorySize > recordOffset {
		return officialZIPPreflightError()
	}
	if (eocdEntriesOnDisk != math.MaxUint16 && uint64(eocdEntriesOnDisk) != entriesOnDisk) ||
		(eocdTotalEntries != math.MaxUint16 && uint64(eocdTotalEntries) != totalEntries) ||
		(eocdDirectorySize != math.MaxUint32 && uint64(eocdDirectorySize) != directorySize) ||
		(eocdDirectoryOffset != math.MaxUint32 && uint64(eocdDirectoryOffset) != directoryOffset) {
		return officialZIPPreflightError()
	}
	return validateOfficialZIPDirectory(reader, directoryOffset, directorySize, recordOffset, totalEntries, maxEntries)
}

func validateOfficialZIPDirectory(
	reader io.ReaderAt,
	directoryOffset, directorySize, metadataOffset, declaredEntries uint64,
	maxEntries int,
) error {
	if directoryOffset > math.MaxUint64-directorySize ||
		directoryOffset+directorySize != metadataOffset ||
		metadataOffset > math.MaxInt64 {
		return officialZIPPreflightError()
	}
	directoryEnd := directoryOffset + directorySize
	entryCount := uint64(0)
	for offset := directoryOffset; offset < directoryEnd; {
		if offset > math.MaxInt64 || directoryEnd-offset < 4 {
			return officialZIPPreflightError()
		}
		var signature [4]byte
		if err := readOfficialZIPAt(reader, signature[:], int64(offset)); err != nil {
			return officialZIPPreflightError()
		}
		switch binary.LittleEndian.Uint32(signature[:]) {
		case zipDirectorySignature:
			if directoryEnd-offset < zipDirectoryHeaderBytes {
				return officialZIPPreflightError()
			}
			var header [zipDirectoryHeaderBytes]byte
			if err := readOfficialZIPAt(reader, header[:], int64(offset)); err != nil {
				return officialZIPPreflightError()
			}
			recordBytes := uint64(zipDirectoryHeaderBytes) +
				uint64(binary.LittleEndian.Uint16(header[28:30])) +
				uint64(binary.LittleEndian.Uint16(header[30:32])) +
				uint64(binary.LittleEndian.Uint16(header[32:34]))
			if recordBytes > directoryEnd-offset {
				return officialZIPPreflightError()
			}
			entryCount++
			if entryCount > uint64(maxEntries) {
				return officialZIPPreflightError()
			}
			offset += recordBytes
		case zipDirectoryDigitalSig:
			if directoryEnd-offset < zipDigitalSigHeaderBytes {
				return officialZIPPreflightError()
			}
			var header [zipDigitalSigHeaderBytes]byte
			if err := readOfficialZIPAt(reader, header[:], int64(offset)); err != nil {
				return officialZIPPreflightError()
			}
			recordBytes := uint64(zipDigitalSigHeaderBytes) + uint64(binary.LittleEndian.Uint16(header[4:6]))
			if recordBytes != directoryEnd-offset {
				return officialZIPPreflightError()
			}
			offset += recordBytes
		default:
			return officialZIPPreflightError()
		}
	}
	if entryCount != declaredEntries {
		return officialZIPPreflightError()
	}
	return nil
}

func findOfficialZIPEOCD(tail []byte) int {
	for index := len(tail) - zipEOCDBytes; index >= 0; index-- {
		if binary.LittleEndian.Uint32(tail[index:index+4]) != zipEOCDSignature {
			continue
		}
		commentBytes := int(binary.LittleEndian.Uint16(tail[index+20 : index+22]))
		if index+zipEOCDBytes+commentBytes == len(tail) {
			return index
		}
	}
	return -1
}

func readOfficialZIPAt(reader io.ReaderAt, data []byte, offset int64) error {
	read, err := reader.ReadAt(data, offset)
	if read != len(data) {
		return io.ErrUnexpectedEOF
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func officialZIPPreflightError() error {
	return &lpkgo.Error{
		Code:  lpkgo.CodeInvalidManifest,
		Op:    "store.official.precheck",
		Cause: errors.New("official LPK ZIP metadata is invalid"),
	}
}
