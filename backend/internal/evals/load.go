package evals

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func LoadCases(root string) ([]Case, error) {
	var cases []Case
	seen := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var evalCase Case
		if err := json.Unmarshal(body, &evalCase); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		if err := evalCase.Validate(); err != nil {
			return fmt.Errorf("validate %s: %w", path, err)
		}
		if previous, exists := seen[evalCase.ID]; exists {
			return fmt.Errorf("duplicate eval case id %q in %s and %s", evalCase.ID, previous, path)
		}
		seen[evalCase.ID] = path
		cases = append(cases, evalCase)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

func (evalCase Case) Validate() error {
	if evalCase.Version != SchemaVersion {
		return fmt.Errorf("version must be %q", SchemaVersion)
	}
	if strings.TrimSpace(evalCase.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if evalCase.Suite != "baseline" && evalCase.Suite != "boundary" && evalCase.Suite != "regression" {
		return fmt.Errorf("suite must be baseline, boundary, or regression")
	}
	if strings.TrimSpace(evalCase.Config.HomeTeam) == "" || strings.TrimSpace(evalCase.Config.AwayTeam) == "" {
		return fmt.Errorf("config homeTeam and awayTeam are required")
	}
	eventKeys := map[string]bool{}
	for _, step := range evalCase.Events {
		if strings.TrimSpace(step.Key) == "" {
			return fmt.Errorf("event key is required")
		}
		if eventKeys[step.Key] {
			return fmt.Errorf("duplicate event key %q", step.Key)
		}
		eventKeys[step.Key] = true
		if step.Corrects != "" && !eventKeys[step.Corrects] {
			return fmt.Errorf("event %q corrects unknown prior key %q", step.Key, step.Corrects)
		}
		if !step.ExpectError && strings.TrimSpace(step.Event.EventType) == "" {
			return fmt.Errorf("event %q eventType is required", step.Key)
		}
	}
	turns := map[string]bool{}
	for _, turn := range evalCase.Turns {
		if strings.TrimSpace(turn.ID) == "" || strings.TrimSpace(turn.UserID) == "" {
			return fmt.Errorf("turn id and userId are required")
		}
		if turns[turn.ID] {
			return fmt.Errorf("duplicate turn id %q", turn.ID)
		}
		turns[turn.ID] = true
	}
	return nil
}

func FilterCases(cases []Case, suite string) []Case {
	suite = strings.TrimSpace(suite)
	if suite == "" || suite == "all" {
		return append([]Case(nil), cases...)
	}
	requested := map[string]bool{}
	for _, value := range strings.Split(suite, ",") {
		requested[strings.TrimSpace(value)] = true
	}
	var filtered []Case
	for _, evalCase := range cases {
		if requested[evalCase.Suite] {
			filtered = append(filtered, evalCase)
		}
	}
	return filtered
}
