package timetable

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentLimits(t *testing.T) {
	t.Run("image dimensions", func(t *testing.T) {
		data, err := os.ReadFile("testdata/grid.png")
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint32(data[16:20], 6000)
		binary.BigEndian.PutUint32(data[20:24], 6000)
		// DecodeConfig verifies the IHDR checksum before reporting dimensions.
		binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
		path := writeFixture(t, "large.png", data)
		if err := checkImage(path); !errors.Is(err, ErrDocument) {
			t.Fatalf("oversized image: %v", err)
		}
	})
	t.Run("populated cells", func(t *testing.T) {
		path := writeFixture(t, "large.csv", []byte(strings.Repeat("a,b,c\n", maxCells/3+1)))
		if _, err := Extract(context.Background(), path, CSV); !errors.Is(err, ErrDocument) {
			t.Fatalf("oversized CSV: %v", err)
		}
	})
	t.Run("expanded workbook archive", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "large.xlsx")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		// A raw ZIP entry declares its uncompressed size in the ZIP directory.
		_, err = zw.CreateRaw(&zip.FileHeader{Name: "xl/workbook.xml", Method: zip.Deflate, UncompressedSize64: maxExpanded + 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if err := Validate(path, XLSX); !errors.Is(err, ErrDocument) {
			t.Fatalf("oversized archive: %v", err)
		}
	})
}

func TestEmptyDraft(t *testing.T) {
	path := writeFixture(t, "empty.csv", []byte("College timetable\nSpring semester\n"))
	result, err := Extract(context.Background(), path, CSV)
	if err != nil {
		t.Fatal(err)
	}
	if result.Entries == nil || len(result.Entries) != 0 || len(result.Warnings) != 1 || result.Warnings[0].Code != "NO_ENTRIES" {
		t.Fatalf("%+v", result)
	}
}

func writeFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
