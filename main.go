package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"sort"
)

type Entry struct {
	Timestamp time.Time
	Level     string
	Service   string
	Message   string
}

type Rule interface {
	Name() string
	Severity() string
	Match(e Entry) bool
}
type FatalRule struct{}
type KeywordRule struct {
	name     string
	severity string
	keyword  string
}

type incidentKey struct {
	Type    string
	Service string
}

type Incident struct {
	Type      string
	Severity  string
	Service   string
	Count     int
	FirstSeen time.Time
	LastSeen  time.Time
}

type Stats struct {
	Valid     int
	Malformed int
	ByLevel   map[string]int
	ByService map[string]int
	Incidents map[incidentKey]*Incident
}


func isvalid(level string) bool {
	return level == "INFO" || level == "WARN" || level == "ERROR" || level == "FATAL"
}

func parseLine(line string) (Entry, error) {
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, " ", 4)

	if len(parts) < 4 {
		return Entry{}, fmt.Errorf("expected 4 fields, got %d", len(parts))
	}

	timestamp, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return Entry{}, fmt.Errorf("bad timestamp: %s", parts[0])
	}

	if !isvalid(parts[1]) {
		return Entry{}, fmt.Errorf("bad level: %s", parts[1])
	}

	if parts[2] == "" {
		return Entry{}, fmt.Errorf("empty service")
	}

	entry := Entry{
		Timestamp: timestamp,
		Level:     parts[1],
		Service:   parts[2],
		Message:   parts[3],
	}
	return entry, nil
}


func readLogFile(path string, rules []Rule) (Stats, error) {
	stats := newStats()

	file, err := os.Open(path)
	if err != nil {
		return stats, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		entry, err := parseLine(scanner.Text())
		if err != nil {
			stats.Malformed++
			continue
		}
		stats.Valid++
		stats.ByLevel[entry.Level]++
		stats.ByService[entry.Service]++
		rule := findRule(rules, entry)
		if rule != nil {
			addIncident(&stats, rule, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return stats, err
	}

	return stats, nil
}

func newStats() Stats {
	return Stats{
		ByLevel:   make(map[string]int),
		ByService: make(map[string]int),
		Incidents: make(map[incidentKey]*Incident),
	}
}

func (r FatalRule) Name() string     { return "Fatal error" }

func (r FatalRule) Severity() string { return "CRITICAL" }

func (r FatalRule) Match(e Entry) bool { return e.Level == "FATAL" }

func (r KeywordRule) Name() string     { return r.name }

func (r KeywordRule) Severity() string { return r.severity }

func (r KeywordRule) Match(e Entry) bool {
	if e.Level != "ERROR" && e.Level != "WARN" { return false }
	return strings.Contains(strings.ToLower(e.Message), r.keyword)
}

func defaultRules() []Rule {
	return []Rule{
		FatalRule{},
		KeywordRule{"Connection failure", "HIGH", "connection refused"},
		KeywordRule{"Timeout", "HIGH", "timeout"},
		KeywordRule{"Retry storm", "MEDIUM", "retry"},
	}
}

func findRule(rules []Rule, e Entry) Rule {
	for _, rule := range rules {
		if rule.Match(e) {
			return rule
		}
	}
	return nil
}



func addIncident(stats *Stats, rule Rule, e Entry) {
	key := incidentKey{Type: rule.Name(), Service: e.Service}

	inc, ok := stats.Incidents[key]
	if !ok {
		stats.Incidents[key] = &Incident{
			Type:      rule.Name(),
			Severity:  rule.Severity(),
			Service:   e.Service,
			Count:     1,
			FirstSeen: e.Timestamp,
			LastSeen:  e.Timestamp,
		}
		return
	}

	inc.Count++
	if e.Timestamp.Before(inc.FirstSeen) {
		inc.FirstSeen = e.Timestamp
	}
	if e.Timestamp.After(inc.LastSeen) {
		inc.LastSeen = e.Timestamp
	}
}

func severityRank(severity string) int {
	switch severity {
	case "CRITICAL":
		return 3
	case "HIGH":
		return 2
	case "MEDIUM":
		return 1
	}
	return 0
}

func sortedIncidents(stats Stats) []Incident {
	list := []Incident{}
	for _, inc := range stats.Incidents {
		list = append(list, *inc)
	}

	sort.Slice(list, func(i, j int) bool {
		a := list[i]
		b := list[j]
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) > severityRank(b.Severity)
		}
		if !a.FirstSeen.Equal(b.FirstSeen) {
			return a.FirstSeen.Before(b.FirstSeen)
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Service < b.Service
	})

	return list
}
func main() {
	outputPath := flag.String("o", "", "write JSON report to this file")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "the foramt: [-o report.json] <logfile>")
		os.Exit(2)
	}
	inputPath := flag.Arg(0)

		stats, err := readLogFile(inputPath, defaultRules())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println("valid entries:", stats.Valid)
	fmt.Println("malformed lines:", stats.Malformed)

	fmt.Println("by level:")
	levels := []string{"INFO", "WARN", "ERROR", "FATAL"}
	for _, level := range levels {
		fmt.Printf("  %s: %d\n", level, stats.ByLevel[level])
	}

	fmt.Println("by service:")
	names := []string{}
	for name := range stats.ByService {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("  %s: %d\n", name, stats.ByService[name])
	}
		fmt.Println("incidents:")
	incidents := sortedIncidents(stats)
	if len(incidents) == 0 {
		fmt.Println("  none")
	}
	for _, inc := range incidents {
		fmt.Printf("  [%s] %s | service=%s | count=%d | %s -> %s\n",
			inc.Severity, inc.Type, inc.Service, inc.Count,
			inc.FirstSeen.Format(time.RFC3339), inc.LastSeen.Format(time.RFC3339))
	}
	_ = outputPath 
}