package timetable

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const maxPages = 40
const maxProcessorOutput = 24 << 20

type processorOutput struct{ bytes.Buffer }

func (w *processorOutput) Write(p []byte) (int, error) {
	if len(p) > maxProcessorOutput-w.Len() {
		return 0, ErrDocument
	}
	return w.Buffer.Write(p)
}

func processor(ctx context.Context, tool string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tool, args...)
	var output processorOutput
	cmd.Stdout = &output
	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(err, exec.ErrNotFound):
		return nil, ErrProcessor
	case err != nil:
		return nil, ErrDocument
	default:
		return output.Bytes(), nil
	}
}

func extractPDF(ctx context.Context, path string) ([]candidate, error) {
	output, err := processor(ctx, "mutool", "show", path, "trailer/Root/Pages/Count", "trailer/Encrypt")
	if err != nil {
		return nil, err
	}
	info := strings.Fields(string(output))
	if len(info) != 2 || info[1] != "null" {
		return nil, ErrDocument
	}
	pages, err := strconv.Atoi(info[0])
	if err != nil || pages < 1 || pages > maxPages {
		return nil, ErrDocument
	}
	var items []candidate
	for page := 1; page <= pages; page++ {
		rows, err := pdfRows(ctx, path, strconv.Itoa(page))
		if err != nil {
			return nil, err
		}
		source := fmt.Sprintf("Page %d", page)
		if len(rows) == 0 {
			rows, err = ocrPage(ctx, path, strconv.Itoa(page))
			if err != nil {
				return nil, err
			}
		}
		items = append(items, recognize(rows, source)...)
	}
	return items, nil
}

func pdfRows(ctx context.Context, path, page string) ([][]string, error) {
	output, err := processor(ctx, "mutool", "draw", "-q", "-F", "stext.json", path, page)
	if err != nil {
		return nil, err
	}
	var document struct {
		Pages []struct {
			Blocks []struct {
				Lines []struct {
					Text string
					BBox struct{ X, Y, W, H float64 }
				}
			}
		}
	}
	if json.Unmarshal(output, &document) != nil || len(document.Pages) != 1 {
		return nil, ErrDocument
	}
	var fragments []fragment
	for _, block := range document.Pages[0].Blocks {
		for _, line := range block.Lines {
			if clean(line.Text) == "" {
				continue
			}
			box := line.BBox
			fragments = append(fragments, fragment{clean(line.Text), box.X, box.Y, box.W, box.H})
		}
	}
	return layoutRows(fragments), nil
}

func ocrPage(ctx context.Context, path, page string) ([][]string, error) {
	rendered := filepath.Join(filepath.Dir(path), "page-"+page+".png")
	defer os.Remove(rendered)
	_, err := processor(ctx, "mutool", "draw", "-q", "-r", "180", "-w", "5000", "-h", "5000", "-o", rendered, path, page)
	if err != nil {
		return nil, err
	}
	if err := checkImage(rendered); err != nil {
		return nil, err
	}
	return imageRows(ctx, rendered)
}

func imageRows(ctx context.Context, path string) ([][]string, error) {
	output, err := processor(ctx, "tesseract", path, "stdout", "-l", "eng", "--psm", "11", "tsv")
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(bytes.NewReader(output))
	reader.Comma = '\t'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	var words []fragment
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrDocument
		}
		if len(row) < 12 || row[0] != "5" || clean(row[11]) == "" {
			continue
		}
		x, _ := strconv.ParseFloat(row[6], 64)
		y, _ := strconv.ParseFloat(row[7], 64)
		w, _ := strconv.ParseFloat(row[8], 64)
		h, _ := strconv.ParseFloat(row[9], 64)
		if w > 0 && h > 0 {
			words = append(words, fragment{clean(row[11]), x, y, w, h})
		}
	}
	return layoutRows(words), nil
}
