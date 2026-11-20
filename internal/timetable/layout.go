package timetable

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strings"
)

type fragment struct {
	text       string
	x, y, w, h float64
}

func (f fragment) centerY() float64 { return f.y + f.h/2 }
func (f fragment) centerX() float64 { return f.x + f.w/2 }

var spacedClock = regexp.MustCompile(`([0-9])\s*:\s*([0-9])`)

func layoutRows(fragments []fragment) [][]string {
	if len(fragments) == 0 {
		return nil
	}
	slices.SortFunc(fragments, func(a, b fragment) int { return cmp.Compare(a.centerY(), b.centerY()) })
	var lines [][]fragment
	for _, item := range fragments {
		if len(lines) > 0 {
			last := lines[len(lines)-1]
			base := last[0]
			if math.Abs(item.centerY()-base.centerY()) <= math.Max(2, 0.52*math.Max(item.h, base.h)) {
				lines[len(lines)-1] = append(last, item)
				continue
			}
		}
		lines = append(lines, []fragment{item})
	}

	var header []fragment
	for i, line := range lines {
		lines[i] = combineNearby(line)
		if header == nil && gridHeader(lines[i]) {
			header = lines[i]
		}
	}
	rows := make([][]string, 0, len(lines))
	for _, line := range lines {
		if header == nil {
			row := make([]string, 0, len(line))
			for _, item := range line {
				row = append(row, item.text)
			}
			rows = append(rows, row)
			continue
		}

		row := make([]string, len(header))
		for _, item := range line {
			col := 0
			for col+1 < len(header) && item.x >= columnBoundary(header[col], header[col+1]) {
				col++
			}
			row[col] = strings.TrimSpace(row[col] + " " + item.text)
		}
		rows = append(rows, row)
	}
	return rows
}

func columnBoundary(left, right fragment) float64 {
	// Use the gap between headings so left-aligned and centered cells both map.
	return (left.x + left.w + right.x) / 2
}

func combineNearby(line []fragment) []fragment {
	slices.SortFunc(line, func(a, b fragment) int { return cmp.Compare(a.x, b.x) })
	var groups []fragment
	for _, item := range line {
		if len(groups) > 0 {
			last := &groups[len(groups)-1]
			if item.x-last.x-last.w < 0.85*math.Max(last.h, item.h) {
				last.text += " " + item.text
				last.w = math.Max(last.w, item.x+item.w-last.x)
				continue
			}
		}
		groups = append(groups, item)
	}
	for i := range groups {
		groups[i].text = spacedClock.ReplaceAllString(groups[i].text, "${1}:${2}")
	}
	return groups
}

func gridHeader(line []fragment) bool {
	if len(line) < 2 {
		return false
	}
	values := make([]string, 0, len(line))
	dayCount, timeCount := 0, 0
	for _, group := range line {
		field := group.text
		values = append(values, field)
		if weekday(field) != 0 {
			dayCount++
		}
		if _, _, span := rangeTimes(field); span != nil && span[0] == 0 && span[1] == len(field) {
			timeCount++
		}
	}
	c := headers(values)
	return (c.day >= 0 && c.subject >= 0 && (c.time >= 0 || (c.start >= 0 && c.end >= 0))) ||
		dayCount >= 2 || (dayCount == 1 && c.time >= 0) || timeCount >= 2 || (timeCount == 1 && c.day >= 0)
}
