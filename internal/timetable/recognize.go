package timetable

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const clockPattern = `([0-9]{1,2})(?::([0-9]{2}))?\s*(am|pm)?`

var (
	timeRange       = regexp.MustCompile(`(?i)\b` + clockPattern + `\s*(?:[-–—]|\bto\b)\s*` + clockPattern + `\b`)
	singleTime      = regexp.MustCompile(`(?i)^` + clockPattern + `$`)
	dayPrefix       = regexp.MustCompile(`(?i)^([a-z]+)\b\s*[:,|]?\s*`)
	roomMarker      = regexp.MustCompile(`(?i)\s+(?:@\s*|room\s*[:#]?\s*)([^;|]+)$`)
	practicalMarker = regexp.MustCompile(`(?i)\b(lab|laboratory|practical)\b`)
	lectureMarker   = regexp.MustCompile(`(?i)\b(lecture|theory)\b`)
	days            = map[string]int{
		"mon": 1, "monday": 1, "tue": 2, "tues": 2, "tuesday": 2, "wed": 3, "wednesday": 3,
		"thu": 4, "thur": 4, "thurs": 4, "thursday": 4, "fri": 5, "friday": 5,
		"sat": 6, "saturday": 6, "sun": 7, "sunday": 7,
	}
)

func weekday(value string) int {
	return days[strings.ToLower(strings.Trim(strings.TrimSpace(value), ".:"))]
}

func clock(groups []string) string {
	if len(groups) != 3 || (groups[1] == "" && groups[2] == "") {
		return ""
	}
	hour, _ := strconv.Atoi(groups[0])
	min, _ := strconv.Atoi(groups[1])
	suffix := strings.ToLower(groups[2])
	if min > 59 || hour > 23 || (suffix != "" && (hour < 1 || hour > 12)) {
		return ""
	}
	if suffix != "" {
		hour %= 12
		if suffix == "pm" {
			hour += 12
		}
	}
	return fmt.Sprintf("%02d:%02d", hour, min)
}

func rangeTimes(value string) (start, end string, indexes []int) {
	indexes = timeRange.FindStringIndex(value)
	if indexes == nil {
		return "", "", nil
	}
	groups := timeRange.FindStringSubmatch(value[indexes[0]:indexes[1]])
	if groups[3] == "" && groups[2] == "" && groups[6] != "" {
		groups[3] = groups[6]
	}
	return clock(groups[1:4]), clock(groups[4:7]), indexes
}

func clean(value string) string { return strings.Join(strings.Fields(value), " ") }

func makeEntry(day int, start, end, subject, room, kind string) Entry {
	subject = clean(strings.Trim(subject, " \t;|,:"))
	if room == "" {
		if match := roomMarker.FindStringSubmatchIndex(subject); match != nil {
			room = subject[match[2]:match[3]]
			subject = subject[:match[0]]
		}
	}
	switch {
	case practicalMarker.MatchString(kind):
		kind = "practical"
	case lectureMarker.MatchString(kind):
		kind = "lecture"
	case practicalMarker.MatchString(subject):
		kind = "practical"
	case lectureMarker.MatchString(subject):
		kind = "lecture"
	default:
		kind = "unknown"
	}
	return Entry{Subject: clean(subject), Weekday: day, StartTime: start, EndTime: end, Room: clean(room), Kind: kind}
}

type columns struct{ day, time, start, end, subject, room, kind int }

func headers(row []string) columns {
	c := columns{-1, -1, -1, -1, -1, -1, -1}
	for i, field := range row {
		switch strings.ToLower(clean(field)) {
		case "day", "weekday":
			c.day = i
		case "time", "hours", "time slot", "timing":
			c.time = i
		case "start", "start time", "from":
			c.start = i
		case "end", "end time", "to":
			c.end = i
		case "subject", "course", "class", "module":
			c.subject = i
		case "room", "venue", "location":
			c.room = i
		case "kind", "type", "session type":
			c.kind = i
		}
	}
	return c
}

func cell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

type gridAxis struct {
	index, day int
	start, end string
}

func recognize(rows [][]string, source string) []candidate {
	for i, row := range rows {
		c := headers(row)
		if c.day >= 0 && c.subject >= 0 && (c.time >= 0 || (c.start >= 0 && c.end >= 0)) {
			return recognizeColumns(rows[i+1:], i+1, source, c)
		}
		var dayColumns, timeColumns []gridAxis
		for j, field := range row {
			if day := weekday(field); day != 0 {
				dayColumns = append(dayColumns, gridAxis{index: j, day: day})
			}
			if start, end, rangeIndex := rangeTimes(strings.TrimSpace(field)); rangeIndex != nil && rangeIndex[0] == 0 && rangeIndex[1] == len(strings.TrimSpace(field)) {
				timeColumns = append(timeColumns, gridAxis{index: j, start: start, end: end})
			}
		}
		if len(dayColumns) >= 2 || (len(dayColumns) == 1 && c.time >= 0) {
			return recognizeGrid(rows[i+1:], i+1, source, dayColumns, true)
		}
		if len(timeColumns) >= 2 || (len(timeColumns) == 1 && c.day >= 0) {
			return recognizeGrid(rows[i+1:], i+1, source, timeColumns, false)
		}
	}
	return recognizeText(rows, source)
}

func recognizeColumns(rows [][]string, offset int, source string, c columns) []candidate {
	var result []candidate
	var day int
	for i, row := range rows {
		label := cell(row, c.day)
		if label != "" {
			day = weekday(label)
		}
		subject := cell(row, c.subject)
		start, end, _ := rangeTimes(cell(row, c.time))
		if c.time < 0 {
			start = cellClock(cell(row, c.start))
			end = cellClock(cell(row, c.end))
		}
		if subject == "" && cell(row, c.time) == "" && cell(row, c.start) == "" && cell(row, c.end) == "" {
			continue
		}
		result = append(result, candidate{makeEntry(day, start, end, subject, cell(row, c.room), cell(row, c.kind)), location(source, offset+i)})
	}
	return result
}

func cellClock(value string) string {
	if groups := singleTime.FindStringSubmatch(value); groups != nil {
		return clock(groups[1:])
	}
	return ""
}

func recognizeGrid(rows [][]string, offset int, source string, axis []gridAxis, byDay bool) []candidate {
	occupied := make(map[int]bool, len(axis))
	for _, column := range axis {
		occupied[column.index] = true
	}
	var result []candidate
	for i, row := range rows {
		var label string
		for j, field := range row {
			if !occupied[j] && strings.TrimSpace(field) != "" {
				label = strings.TrimSpace(field)
				break
			}
		}
		if label == "" {
			continue
		}
		rowDay := weekday(label)
		rowStart, rowEnd, _ := rangeTimes(label)
		for _, column := range axis {
			subject := cell(row, column.index)
			switch strings.ToLower(subject) {
			case "", "-", "—", "break", "lunch", "free":
				continue
			}
			day, start, end := rowDay, column.start, column.end
			if byDay {
				day, start, end = column.day, rowStart, rowEnd
			}
			result = append(result, candidate{makeEntry(day, start, end, subject, "", ""), location(source, offset+i)})
		}
	}
	return result
}

func recognizeText(rows [][]string, source string) []candidate {
	var result []candidate
	var day int
	for i, row := range rows {
		line := clean(strings.Join(row, " "))
		if match := dayPrefix.FindStringSubmatchIndex(line); match != nil {
			if parsed := weekday(line[match[2]:match[3]]); parsed != 0 {
				day = parsed
				line = line[match[1]:]
			}
		}
		if line == "" {
			continue
		}
		start, end, span := rangeTimes(line)
		if span == nil {
			// Flag partial schedule lines while ignoring titles and narrative prose.
			if day != 0 && strings.Contains(line, ":") {
				result = append(result, candidate{makeEntry(day, "", "", line, "", ""), location(source, i)})
			}
			continue
		}
		subject := strings.TrimSpace(line[span[1]:])
		if subject == "" {
			subject = line[:span[0]]
		}
		result = append(result, candidate{makeEntry(day, start, end, subject, "", ""), location(source, i)})
	}
	return result
}

func location(source string, index int) string {
	if strings.HasPrefix(source, "Page ") {
		return source
	}
	return fmt.Sprintf("%s, row %d", source, index+1)
}
