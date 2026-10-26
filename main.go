package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"bufio"
)

type Entry struct {
	Timestamp time.Time
	Level     string
	Service   string
	Message   string
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

func readLogFile(path string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// allow lines up to 1 MB (default is only 64 KB)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	valid := 0
	malformed := 0

	for scanner.Scan() {
		line := scanner.Text()
		_, err := parseLine(line)
		if err != nil {
			malformed++
			continue
		}
		valid++
	}

	// scanner stops silently on errors, so we must check
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	return valid, malformed, nil
}

func main() {
	outputPath := flag.String("o", "", "write JSON report to this file")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "the foramt: [-o report.json] <logfile>")
		os.Exit(2)
	}
	inputPath := flag.Arg(0)

	valid, malformed, err := readLogFile(inputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println("valid entries:", valid)
	fmt.Println("malformed lines:", malformed)
	_ = outputPath // used in a later step
}