package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Valid != 2 {
		t.Errorf("valid = %d, want 2", stats.Valid)
	}
	if stats.Malformed != 2 {
		t.Errorf("malformed = %d, want 2", stats.Malformed)
	}
}

func TestReadLogFileMissing(t *testing.T) {
	_, err := readLogFile("does-not-exist.log", defaultRules())
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadLogFileEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	os.WriteFile(path, []byte(""), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Valid != 0 || stats.Malformed != 0 {
		t.Errorf("got %d valid, %d malformed, want 0 and 0", stats.Valid, stats.Malformed)
	}
	if len(stats.ByLevel) != 0 || len(stats.ByService) != 0 {
		t.Errorf("maps should be empty")
	}
}

func TestStatsCounts(t *testing.T) {
	content := "2026-10-26T10:00:00Z INFO auth login\n" +
		"2026-10-26T10:00:01Z INFO api ok\n" +
		"2026-10-26T10:00:02Z ERROR auth failed\n" +
		"2026-10-26T10:00:03Z FATAL auth crashed\n" +
		"bad line\n"

	path := filepath.Join(t.TempDir(), "stats.log")
	os.WriteFile(path, []byte(content), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stats.Valid != 4 || stats.Malformed != 1 {
		t.Errorf("valid=%d malformed=%d, want 4 and 1", stats.Valid, stats.Malformed)
	}
	if stats.ByLevel["INFO"] != 2 || stats.ByLevel["ERROR"] != 1 || stats.ByLevel["FATAL"] != 1 || stats.ByLevel["WARN"] != 0 {
		t.Errorf("wrong level counts: %v", stats.ByLevel)
	}
	if stats.ByService["auth"] != 3 || stats.ByService["api"] != 1 {
		t.Errorf("wrong service counts: %v", stats.ByService)
	}
}

func TestFindRule(t *testing.T) {
	rules := defaultRules()

	tests := []struct {
		name  string
		level string
		msg   string
		want  string 
	}{
		{"fatal", "FATAL", "Out of memory", "Fatal error"},
		{"timeout", "ERROR", "Database connection timeout", "Timeout"},
		{"timeout uppercase", "ERROR", "REQUEST TIMEOUT", "Timeout"},
		{"connection refused", "ERROR", "connection refused", "Connection failure"},
		{"retry", "WARN", "Retry attempt 2", "Retry storm"},
		{"info is ignored", "INFO", "timeout happened", ""},
		{"no keyword", "ERROR", "something else", ""},
		{"fatal beats timeout", "FATAL", "crashed after timeout", "Fatal error"},
		{"refused beats timeout", "ERROR", "connection refused after timeout", "Connection failure"},
		{"timeout beats retry", "WARN", "retry failed: timeout", "Timeout"},
	}

	for _, tt := range tests {
		entry := Entry{Level: tt.level, Service: "api", Message: tt.msg}
		rule := findRule(rules, entry)

		if tt.want == "" {
			if rule != nil {
				t.Errorf("%s: expected no rule, got %s", tt.name, rule.Name())
			}
			continue
		}
		if rule == nil {
			t.Errorf("%s: expected %s, got no rule", tt.name, tt.want)
			continue
		}
		if rule.Name() != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, rule.Name(), tt.want)
		}
	}
}

func TestIncidentGrouping(t *testing.T) {
	content := "2026-10-26T10:00:05Z ERROR auth Database timeout\n" +
		"2026-10-26T10:00:01Z ERROR auth Request timeout\n" + 
		"2026-10-26T10:00:09Z ERROR api Request timeout\n" +
		"2026-10-26T10:00:10Z FATAL auth crashed with timeout\n" +
		"2026-10-26T10:00:11Z INFO auth timeout ignored\n"

	path := filepath.Join(t.TempDir(), "inc.log")
	os.WriteFile(path, []byte(content), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stats.Incidents) != 3 {
		t.Fatalf("got %d incidents, want 3", len(stats.Incidents))
	}

	inc := stats.Incidents[incidentKey{"Timeout", "auth"}]
	if inc == nil {
		t.Fatal("missing Timeout/auth incident")
	}
	if inc.Count != 2 {
		t.Errorf("count = %d, want 2", inc.Count)
	}
	if inc.FirstSeen.Format(time.RFC3339) != "2026-10-26T10:00:01Z" {
		t.Errorf("first seen = %v", inc.FirstSeen)
	}
	if inc.LastSeen.Format(time.RFC3339) != "2026-10-26T10:00:05Z" {
		t.Errorf("last seen = %v", inc.LastSeen)
	}

	fatal := stats.Incidents[incidentKey{"Fatal error", "auth"}]
	if fatal == nil || fatal.Count != 1 {
		t.Errorf("fatal incident wrong: %+v", fatal)
	}
}

func TestSortedIncidents(t *testing.T) {
	content := "2026-10-26T10:00:01Z WARN a Retry 1\n" +
		"2026-10-26T10:00:02Z ERROR b request timeout\n" +
		"2026-10-26T10:00:03Z FATAL c boom\n" +
		"2026-10-26T10:00:00Z ERROR d connection refused\n"

	path := filepath.Join(t.TempDir(), "sort.log")
	os.WriteFile(path, []byte(content), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sortedIncidents(stats)
	want := []string{"Fatal error", "Connection failure", "Timeout", "Retry storm"}
	if len(got) != len(want) {
		t.Fatalf("got %d incidents, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Type != want[i] {
			t.Errorf("position %d: got %s, want %s", i, got[i].Type, want[i])
		}
	}
}

func TestNoIncidents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.log")
	os.WriteFile(path, []byte("2026-10-26T10:00:00Z INFO api ok\n"), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sortedIncidents(stats)) != 0 {
		t.Error("expected no incidents")
	}
}