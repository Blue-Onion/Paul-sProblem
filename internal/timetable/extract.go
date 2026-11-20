package timetable

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Format string

const (
	CSV  Format = "csv"
	XLSX Format = "xlsx"
	XLS  Format = "xls"
	PDF  Format = "pdf"
	PNG  Format = "png"
	JPEG Format = "jpg"

	maxPixels   = 25_000_000
	maxCells    = 100_000
	maxExpanded = 64 << 20
)

func FromFilename(name string) Format {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv":
		return CSV
	case ".xlsx":
		return XLSX
	case ".xls":
		return XLS
	case ".pdf":
		return PDF
	case ".png":
		return PNG
	case ".jpg", ".jpeg":
		return JPEG
	default:
		return ""
	}
}

func Validate(path string, format Format) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var header [512]byte
	n, err := io.ReadFull(f, header[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	data := header[:n]
	switch format {
	case PDF:
		if bytes.HasPrefix(data, []byte("%PDF-")) {
			return nil
		}
	case PNG:
		if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
			return nil
		}
	case JPEG:
		if bytes.HasPrefix(data, []byte("\xff\xd8\xff")) {
			return nil
		}
	case XLS:
		if ole(data) {
			if err := identifyLegacy(f); err != nil {
				return err
			}
			return nil
		}
	case XLSX:
		if ole(data) {
			if err := identifyLegacy(f); errors.Is(err, ErrDocument) {
				return err
			}
		}
		if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
			archive, err := zip.OpenReader(path)
			if err != nil {
				return ErrDocument
			}
			defer archive.Close()
			hasWorkbook := false
			var size uint64
			for _, file := range archive.File {
				if file.Name == "xl/workbook.xml" {
					hasWorkbook = true
				}
				if file.UncompressedSize64 > maxExpanded || size > maxExpanded-file.UncompressedSize64 {
					return ErrDocument
				}
				size += file.UncompressedSize64
			}
			if hasWorkbook {
				return nil
			}
		}
	case CSV:
		kind := http.DetectContentType(data)
		if strings.HasPrefix(kind, "text/") && !strings.HasPrefix(kind, "text/html") {
			return nil
		}
	}
	return ErrFormat
}

func ole(data []byte) bool { return bytes.HasPrefix(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")) }

func Extract(ctx context.Context, path string, format Format) (Result, error) {
	var items []candidate
	var err error
	switch format {
	case CSV:
		items, err = extractCSV(ctx, path)
	case XLSX:
		items, err = extractXLSX(ctx, path)
	case XLS:
		items, err = extractXLS(ctx, path)
	case PDF:
		items, err = extractPDF(ctx, path)
	case PNG, JPEG:
		if err = checkImage(path); err == nil {
			var rows [][]string
			rows, err = imageRows(ctx, path)
			if err == nil {
				items = recognize(rows, "Page 1")
			}
		}
	default:
		err = ErrFormat
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err != nil {
		return Result{}, err
	}
	return normalize(items), nil
}

func extractCSV(ctx context.Context, path string) ([]candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, ErrDocument
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	var rows [][]string
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrDocument
		}
		if populated(row, &count) {
			return nil, ErrDocument
		}
		rows = append(rows, row)
	}
	return recognize(rows, "CSV"), nil
}

func populated(row []string, count *int) bool {
	for _, field := range row {
		if strings.TrimSpace(field) != "" {
			*count = *count + 1
		}
	}
	return *count > maxCells
}

func checkImage(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	config, _, err := image.DecodeConfig(f)
	if err != nil || config.Height <= 0 || config.Width <= 0 || config.Width > maxPixels/config.Height {
		return ErrDocument
	}
	return nil
}
