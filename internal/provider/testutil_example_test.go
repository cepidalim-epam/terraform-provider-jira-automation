package provider

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the absolute path to the repository root.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// loadExample reads an example .tf file from examples/resources/jira-automation_rule/
// and substitutes placeholder values with test-specific values.
func loadExample(t *testing.T, filename string, replacements map[string]string) string {
	t.Helper()

	path := filepath.Join(repoRoot(), "examples", "resources", "jira-automation_rule", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading example file %s: %v", filename, err)
	}

	result := string(data)
	for old, new := range replacements {
		result = strings.ReplaceAll(result, old, new)
	}
	return result
}

// defaultReplacements returns the standard placeholder→real-value map for doc example tests.
// exampleName should include the surrounding quotes as they appear in the .tf file,
// e.g. `"Log on transition"`.
func defaultReplacements(testName string, exampleName string) map[string]string {
	return map[string]string{
		exampleName:                         `"` + testName + `"`,
		`project_id = "10001"`:              `project_id = "` + os.Getenv("JIRA_TEST_PROJECT_ID") + `"`,
		`version_field = "release_version"`: `version_field = "customfield_10020"`,
	}
}
