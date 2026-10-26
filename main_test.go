package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestReadLogFileIsDirectory(t *testing.T) {
	_, err := readLogFile(t.TempDir(), defaultRules())
	if err == nil {
		t.Error("expected error when path is a directory")
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

func TestReadLogFileLongLine(t *testing.T) {
	message := strings.Repeat("x", 200*1024)
	path := filepath.Join(t.TempDir(), "long.log")
	os.WriteFile(path, []byte("2026-10-26T10:00:00Z INFO api "+message+"\n"), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Valid != 1 {
		t.Errorf("valid = %d, want 1", stats.Valid)
	}
}

func TestReadLogFileTooLongLine(t *testing.T) {
	message := strings.Repeat("x", 2*1024*1024)
	path := filepath.Join(t.TempDir(), "toolong.log")
	os.WriteFile(path, []byte("2026-10-26T10:00:00Z INFO api "+message+"\n"), 0644)

	_, err := readLogFile(path, defaultRules())
	if err == nil {
		t.Error("expected error for line longer than 1 MB")
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

func TestTextReportEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	os.WriteFile(path, []byte(""), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = writeTextReport(&buf, buildReport(stats))
	if err != nil {
		t.Fatal(err)
	}

	want := "valid entries: 0\n" +
		"malformed lines: 0\n" +
		"by level:\n" +
		"  INFO: 0\n" +
		"  WARN: 0\n" +
		"  ERROR: 0\n" +
		"  FATAL: 0\n" +
		"by service:\n" +
		"  none\n" +
		"incidents:\n" +
		"  none\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestTextReportOnlyMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.log")
	os.WriteFile(path, []byte("garbage\nmore garbage\n"), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	writeTextReport(&buf, buildReport(stats))

	if !strings.Contains(buf.String(), "valid entries: 0") ||
		!strings.Contains(buf.String(), "malformed lines: 2") {
		t.Errorf("unexpected report:\n%s", buf.String())
	}
}

func TestSampleFile(t *testing.T) {
	stats, err := readLogFile("testdata/sample.log", defaultRules())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	report := buildReport(stats)

	if report.ValidEntries != 11 || report.MalformedLines != 3 {
		t.Errorf("valid=%d malformed=%d, want 11 and 3", report.ValidEntries, report.MalformedLines)
	}
	if len(report.Incidents) != 5 {
		t.Fatalf("got %d incidents, want 5", len(report.Incidents))
	}

	var buf bytes.Buffer
	err = writeTextReport(&buf, report)
	if err != nil {
		t.Fatal(err)
	}
	text := buf.String()

	wantLines := []string{
		"  INFO: 2",
		"  WARN: 4",
		"  ERROR: 3",
		"  FATAL: 2",
		"  api-gateway: 5",
		"  auth-service: 4",
		"  payment-service: 2",
		"  [CRITICAL] Fatal error | service=payment-service | count=1 | 2026-10-26T10:03:30Z -> 2026-10-26T10:03:30Z",
		"  [HIGH] Timeout | service=auth-service | count=2 | 2026-10-26T10:01:10Z -> 2026-10-26T10:07:00Z",
		"  [MEDIUM] Retry storm | service=api-gateway | count=3 | 2026-10-26T10:06:00Z -> 2026-10-26T10:06:10Z",
	}
	for _, line := range wantLines {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("report is missing line: %q", line)
		}
	}
}

func TestTextReportIsDeterministic(t *testing.T) {
	first := ""
	for i := 0; i < 20; i++ {
		stats, err := readLogFile("testdata/sample.log", defaultRules())
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		writeTextReport(&buf, buildReport(stats))

		if i == 0 {
			first = buf.String()
		} else if buf.String() != first {
			t.Fatal("report changed between runs")
		}
	}
}

func TestWriteJSONFile(t *testing.T) {
	stats, err := readLogFile("testdata/sample.log", defaultRules())
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "report.json")
	err = writeJSONFile(path, buildReport(stats))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var got Report
	err = json.Unmarshal(data, &got)
	if err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.ValidEntries != 11 || got.MalformedLines != 3 {
		t.Errorf("wrong counts in JSON: %+v", got)
	}
	if len(got.Incidents) != 5 {
		t.Errorf("got %d incidents in JSON, want 5", len(got.Incidents))
	}
	if got.ByLevel["WARN"] != 4 {
		t.Errorf("by_level WARN = %d, want 4", got.ByLevel["WARN"])
	}
}

func TestWriteJSONFileEmptyReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	os.WriteFile(path, []byte(""), 0644)

	stats, err := readLogFile(path, defaultRules())
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "empty.json")
	err = writeJSONFile(out, buildReport(stats))
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "null") {
		t.Errorf("JSON contains null:\n%s", data)
	}
	if !strings.Contains(string(data), `"incidents": []`) {
		t.Errorf("expected empty incidents list:\n%s", data)
	}
}

func TestWriteJSONFileBadPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "report.json")
	err := writeJSONFile(path, buildReport(newStats()))
	if err == nil {
		t.Error("expected error when output directory does not exist")
	}
}