package timetable

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/richardlehane/mscfb"
	legacy "github.com/shakinm/xlsReader/xls"
	"github.com/xuri/excelize/v2"
)

func extractXLSX(ctx context.Context, path string) ([]candidate, error) {
	book, err := excelize.OpenFile(path, excelize.Options{UnzipSizeLimit: maxExpanded, UnzipXMLSizeLimit: 8 << 20, TmpDir: filepath.Dir(path)})
	if err != nil {
		return nil, ErrDocument
	}
	defer book.Close()
	var items []candidate
	count := 0
	for sheetIndex, name := range book.GetSheetList() {
		rows, err := book.Rows(name)
		if err != nil {
			return nil, ErrDocument
		}
		values, err := readWorksheet(ctx, rows, &count)
		if err != nil {
			return nil, err
		}
		items = append(items, recognize(values, fmt.Sprintf("Sheet %d", sheetIndex+1))...)
	}
	return items, nil
}

func readWorksheet(ctx context.Context, rows *excelize.Rows, count *int) ([][]string, error) {
	defer rows.Close()
	var values [][]string
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row, err := rows.Columns()
		if err != nil || populated(row, count) {
			return nil, ErrDocument
		}
		values = append(values, row)
	}
	if rows.Error() != nil {
		return nil, ErrDocument
	}
	return values, nil
}

func identifyLegacy(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	container, err := mscfb.New(f)
	if err != nil {
		return ErrDocument
	}
	for {
		part, err := container.Next()
		if errors.Is(err, io.EOF) {
			return ErrFormat
		}
		if err != nil {
			return ErrDocument
		}
		switch part.Name {
		case "Workbook", "Book":
			if part.Size <= 0 || part.Size > info.Size() {
				return ErrDocument
			}
			return validateBIFF(part)
		case "EncryptionInfo", "EncryptedPackage":
			return ErrDocument
		}
	}
}

func validateBIFF(stream io.Reader) error {
	var header [4]byte
	first := true
	for {
		if _, err := io.ReadFull(stream, header[:]); err != nil {
			return ErrDocument
		}
		id := binary.LittleEndian.Uint16(header[:2])
		size := binary.LittleEndian.Uint16(header[2:])
		if first && id != 0x0809 {
			return ErrDocument
		}
		first = false
		switch id {
		case 0x002f: // FILEPASS marks an encrypted BIFF workbook.
			return ErrDocument
		case 0x000a: // EOF at the end of workbook globals.
			if size != 0 {
				return ErrDocument
			}
			return nil
		}
		if _, err := io.CopyN(io.Discard, stream, int64(size)); err != nil {
			return ErrDocument
		}
	}
}

func extractXLS(ctx context.Context, path string) (items []candidate, err error) {
	// Invalid BIFF record indexes can panic inside the legacy parser.
	defer func() {
		if recover() != nil {
			items, err = nil, ErrDocument
		}
	}()
	book, err := legacy.OpenFile(path)
	if err != nil {
		return nil, ErrDocument
	}
	count := 0
	for i := 0; i < book.GetNumberSheets(); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sheet, err := book.GetSheet(i)
		if err != nil {
			return nil, ErrDocument
		}
		var values [][]string
		rowCount := sheet.GetNumberRows()
		for j := 0; j < rowCount; j++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			row, err := sheet.GetRow(j)
			if err != nil {
				values = append(values, nil)
				continue
			}
			var value []string
			for _, col := range row.GetCols() {
				value = append(value, col.GetString())
			}
			if populated(value, &count) {
				return nil, ErrDocument
			}
			values = append(values, value)
		}
		items = append(items, recognize(values, fmt.Sprintf("Sheet %d", i+1))...)
	}
	return items, nil
}
