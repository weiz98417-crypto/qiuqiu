package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"qiuqiu/internal/evals"
)

func main() {
	casesDir := flag.String("cases", findCasesDir(), "directory containing versioned eval cases")
	suite := flag.String("suite", "all", "baseline, boundary, regression, a comma-separated list, or all")
	out := flag.String("out", "", "optional JSON report path")
	flag.Parse()

	cases, err := evals.LoadCases(*casesDir)
	if err != nil {
		fail(err)
	}
	cases = evals.FilterCases(cases, *suite)
	if len(cases) == 0 {
		fail(fmt.Errorf("no cases selected for suite %q", *suite))
	}

	report := evals.Run(context.Background(), cases)
	report.GitRevision = gitRevision()
	if err := writeJSON(*out, report); err != nil {
		fail(err)
	}
	if report.Scorecard.FailedCases > 0 {
		os.Exit(1)
	}
}

func findCasesDir() string {
	for _, path := range []string{
		filepath.Join("evals", "cases"),
		filepath.Join("..", "evals", "cases"),
	} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	return filepath.Join("evals", "cases")
}

func writeJSON(path string, value any) error {
	var target *os.File
	if path == "" {
		target = os.Stdout
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
		target = file
	}
	encoder := json.NewEncoder(target)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "evals:", err)
	os.Exit(2)
}

func gitRevision() string {
	output, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return string(output[:len(output)-1])
}
