package timetable

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestDocumentFixtures(t *testing.T) {
	for _, test := range []struct {
		name       string
		format     Format
		processors []string
		warnings   int
	}{
		{"grid.pdf", PDF, []string{"mutool"}, 0},
		{"mixed.pdf", PDF, []string{"mutool", "tesseract"}, 3},
		{"grid.png", PNG, []string{"tesseract"}, 0},
		{"grid.jpg", JPEG, []string{"tesseract"}, 0},
		{"grid.xls", XLS, nil, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, tool := range test.processors {
				if _, err := exec.LookPath(tool); err != nil {
					if os.Getenv("REQUIRE_DOCUMENT_TOOLS") == "1" {
						t.Fatal(err)
					}
					t.Skip(err)
				}
			}
			path := filepath.Join("testdata", test.name)
			if err := Validate(path, test.format); err != nil {
				t.Fatal(err)
			}
			// PDF rasterization writes inside the request directory.
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			path = filepath.Join(t.TempDir(), test.name)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			result, err := Extract(context.Background(), path, test.format)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Entries) != 3 || len(result.Warnings) != test.warnings {
				t.Fatalf("%+v", result)
			}
			if physics := result.Entries[0]; physics.Subject != "Physics" || physics.Weekday != 1 || physics.StartTime != "09:00" || physics.EndTime != "10:00" {
				t.Fatalf("%+v", physics)
			}
			if lab := result.Entries[1]; lab.Subject != "Chemistry Lab" || lab.Kind != "practical" {
				t.Fatalf("%+v", lab)
			}
			if history := result.Entries[2]; history.Subject != "History" || history.Weekday != 3 {
				t.Fatalf("%+v", history)
			}
		})
	}
}

func TestXLSX(t *testing.T) {
	book := excelize.NewFile()
	defer book.Close()
	if _, err := book.NewSheet("Second"); err != nil {
		t.Fatal(err)
	}
	for _, sheet := range []string{"Sheet1", "Second"} {
		day := "Monday"
		if sheet == "Second" {
			day = "Friday"
		}
		for i, row := range [][]any{{"Day", "Start", "End", "Subject", "Room", "Type"}, {day, "09:00", "10:00", "Physics", "Lab 2", "Practical"}} {
			address, err := excelize.CoordinatesToCellName(1, i+1)
			if err != nil {
				t.Fatal(err)
			}
			if err := book.SetSheetRow(sheet, address, &row); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := filepath.Join(t.TempDir(), "input.xlsx")
	if err := book.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	if err := Validate(path, XLSX); err != nil {
		t.Fatal(err)
	}
	result, err := Extract(context.Background(), path, XLSX)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 2 || len(result.Warnings) != 0 || result.Entries[1].Weekday != 5 || result.Entries[1].Room != "Lab 2" {
		t.Fatalf("%+v", result)
	}

	encryptedPath := filepath.Join(t.TempDir(), "encrypted.xlsx")
	if err := book.SaveAs(encryptedPath, excelize.Options{Password: "test-password"}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(encryptedPath, XLSX); !errors.Is(err, ErrDocument) {
		t.Fatalf("encrypted workbook: %v", err)
	}
}
