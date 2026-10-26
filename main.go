package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	outputPath := flag.String("o", "", "write JSON report to this file")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "the foramt: [-o report.json] <logfile>")
		os.Exit(2)
	}
	inputPath := flag.Arg(0)

	fmt.Println("input:", inputPath)
	fmt.Println("output:", *outputPath)
}