package timetable

import (
	"cmp"
	"errors"
	"slices"
	"strings"
)

type Result struct {
	Entries  []Entry   `json:"entries"`
	Warnings []Warning `json:"warnings"`
}

type Entry struct {
	Subject   string `json:"subject"`
	Weekday   int    `json:"weekday"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Room      string `json:"room,omitempty"`
	Kind      string `json:"kind"`
}

type Warning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

type candidate struct {
	entry    Entry
	location string
}

var (
	ErrFormat    = errors.New("unrecognized file format")
	ErrDocument  = errors.New("unprocessible document")
	ErrProcessor = errors.New("missing document processor")
)

func normalize(candidates []candidate) Result {
	result := Result{Entries: []Entry{}, Warnings: []Warning{}}
	seen := make(map[Entry]bool)
	for _, item := range candidates {
		entry := item.entry
		if entry.Subject == "" || entry.Weekday == 0 || entry.StartTime == "" || entry.EndTime <= entry.StartTime {
			result.Warnings = append(result.Warnings, Warning{"AMBIGUOUS_ENTRY", "Could not determine the subject, weekday, or same-day time range", item.location})
			continue
		}
		key := entry
		key.Subject = strings.ToLower(key.Subject)
		key.Room = strings.ToLower(key.Room)
		if seen[key] {
			result.Warnings = append(result.Warnings, Warning{"DUPLICATE_ENTRY", "Ignored a duplicate class", item.location})
			continue
		}
		seen[key] = true
		result.Entries = append(result.Entries, entry)
	}
	slices.SortFunc(result.Entries, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(a.Weekday, b.Weekday), cmp.Compare(a.StartTime, b.StartTime), cmp.Compare(a.EndTime, b.EndTime), cmp.Compare(a.Subject, b.Subject), cmp.Compare(a.Room, b.Room), cmp.Compare(a.Kind, b.Kind))
	})
	if len(result.Entries) == 0 {
		result.Warnings = append(result.Warnings, Warning{"NO_ENTRIES", "No usable classes were recognized", ""})
	}
	return result
}
