package timetable

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCSVExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.csv")
	csv := "\ufeffDay,Time,Subject,Room,Type\nMonday,9:00 AM - 10:00 AM,\"Physics, I\",B2,Lab\nMonday,9:00 AM - 10:00 AM,\"physics, i\",b2,Lab\nMonday,12:00 PM - 1:00 PM,\"Physics, I\",,\nSunday,15:00 - 16:00,Algorithms,,Lecture\nTuesday,09:00,Incomplete,,\n"
	if err := os.WriteFile(path, []byte(csv), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Extract(context.Background(), path, CSV)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 3 || len(result.Warnings) != 2 {
		t.Fatalf("%+v", result)
	}
	if got := result.Entries[0]; got.Subject != "Physics, I" || got.Room != "B2" || got.Kind != "practical" || got.StartTime != "09:00" {
		t.Fatalf("%+v", got)
	}
	if got := result.Entries[1]; got.Kind != "unknown" || got.StartTime != "12:00" || got.EndTime != "13:00" {
		t.Fatalf("%+v", got)
	}
	if result.Entries[2].Weekday != 7 {
		t.Fatal(result.Entries)
	}
}

func TestGrids(t *testing.T) {
	for _, rows := range [][][]string{
		{{"Day", "09:00-10:00", "11:00-12:00"}, {"Mon", "Physics", "Chemistry Lab"}, {"Sat", "History", ""}},
		{{"Time", "Mon", "Sat"}, {"09:00-10:00", "Physics", "History"}, {"11:00-12:00", "Chemistry Lab", ""}},
	} {
		result := normalize(recognize(rows, "Sheet 1"))
		if len(result.Entries) != 3 || result.Entries[1].Kind != "practical" || result.Entries[2].Weekday != 6 {
			t.Fatalf("%+v", result)
		}
	}
}

func TestText(t *testing.T) {
	result := normalize(recognize([][]string{{"Tuesday"}, {"09:00 – 10:00 Databases Lecture Room 204"}, {"16:00-15:00 Invalid"}, {"Sunday 14:30-16:00 Robotics Practical @ Workshop"}}, "Page 1"))
	if len(result.Entries) != 2 || len(result.Warnings) != 1 {
		t.Fatalf("%+v", result)
	}
	if result.Entries[0].Room != "204" || result.Entries[1].Room != "Workshop" {
		t.Fatal(result.Entries)
	}
}
