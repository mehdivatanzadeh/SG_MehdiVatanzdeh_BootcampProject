package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLineValid(t *testing.T) {
	line := "2026-10-26T14:03:22Z ERROR auth-service Database connection timeout"
	entry, err := parseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Level != "ERROR" {
		t.Errorf("level = %s, want ERROR", entry.Level)
	}
	if entry.Service != "auth-service" {
		t.Errorf("service = %s, want auth-service", entry.Service)
	}
	if entry.Message != "Database connection timeout" {
		t.Errorf("message = %s", entry.Message)
	}
}

func TestParseLineWindowsLineEnding(t *testing.T) {
	entry, err := parseLine("2026-10-26T14:03:22Z INFO api started\r")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Message != "started" {
		t.Errorf("message = %q, want started", entry.Message)
	}
}

func TestParseLineMalformed(t *testing.T) {
	badLines := []string{
		"",
		"2026-10-26T14:03:22Z INFO api",
		"yesterday INFO api started",
		"2026-10-26T14:03:22Z DEBUG api started",
		"2026-10-26T14:03:22Z info api started",
		"hello world",
	}
	for _, line := range badLines {
		_, err := parseLine(line)
		if err == nil {
			t.Errorf("expected error for line %q", line)
		}
	}
}

func TestReadLogFile(t *testing.T) {
	content := "2026-10-26T10:00:00Z INFO api ok\n" +
		"garbage line\n" +
		"\n" +
		"2026-10-26T10:00:01Z ERROR api failed\n"

	path := filepath.Join(t.TempDir(), "test.log")
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	valid, malformed, err := readLogFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid != 2 {
		t.Errorf("valid = %d, want 2", valid)
	}
	if malformed != 2 {
		t.Errorf("malformed = %d, want 2", malformed)
	}
}

func TestReadLogFileMissing(t *testing.T) {
	_, _, err := readLogFile("does-not-exist.log")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadLogFileEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	os.WriteFile(path, []byte(""), 0644)

	valid, malformed, err := readLogFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid != 0 || malformed != 0 {
		t.Errorf("got %d valid, %d malformed, want 0 and 0", valid, malformed)
	}
}