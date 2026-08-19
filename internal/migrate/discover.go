package migrate

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var versionedName = regexp.MustCompile(`^V([0-9]+)__(.+)\.sql$`)

type Migration struct {
	Version     int
	VersionText string
	Description string
	Script      string
	SQL         []byte
	Checksum    int32
}

func Discover(source fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	result := make([]Migration, 0, len(entries))
	seen := make(map[int]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := versionedName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, parseErr := strconv.Atoi(match[1])
		if parseErr != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %s", entry.Name())
		}
		if previous, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d: %s and %s", version, previous, entry.Name())
		}
		sql, readErr := fs.ReadFile(source, entry.Name())
		if readErr != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), readErr)
		}
		seen[version] = entry.Name()
		result = append(result, Migration{
			Version: version, VersionText: match[1],
			Description: strings.ReplaceAll(match[2], "_", " "),
			Script:      entry.Name(), SQL: sql, Checksum: Checksum(sql),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

// Checksum reproduces Flyway's versioned SQL checksum: UTF-8 CRC32 over each
// logical line without line-ending bytes. CRC32 is stored as a signed Java int.
func Checksum(source []byte) int32 {
	hash := crc32.NewIEEE()
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if first {
			line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
			first = false
		}
		_, _ = hash.Write(line)
	}
	return int32(hash.Sum32())
}
