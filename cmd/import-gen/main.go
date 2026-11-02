package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"terraform-provider-jira-automation/internal/client"
)

// extractUUIDFromURL extracts a rule UUID from a Jira Automation URL.
// Expects a fragment like #/rule/<uuid> at the end.
var ruleURLPattern = regexp.MustCompile(`#/rule/([0-9a-f-]+)`)

func extractUUIDFromURL(url string) string {
	m := ruleURLPattern.FindStringSubmatch(url)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func main() {
	outDir := "."
	labelFilter := ""
	ruleID := ""

	// Parse flags.
	args := os.Args[1:]
	var positional []string
	for i := 0; i < len(args); i++ {
		switch {
		case (args[i] == "--label") && i+1 < len(args):
			labelFilter = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--label="):
			labelFilter = strings.TrimPrefix(args[i], "--label=")
		case (args[i] == "--id") && i+1 < len(args):
			ruleID = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--id="):
			ruleID = strings.TrimPrefix(args[i], "--id=")
		case (args[i] == "--url") && i+1 < len(args):
			ruleID = extractUUIDFromURL(args[i+1])
			if ruleID == "" {
				log.Fatalf("Could not extract rule UUID from URL: %s", args[i+1])
			}
			i++
		case strings.HasPrefix(args[i], "--url="):
			ruleID = extractUUIDFromURL(strings.TrimPrefix(args[i], "--url="))
			if ruleID == "" {
				log.Fatalf("Could not extract rule UUID from URL: %s", args[i])
			}
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) > 0 {
		outDir = positional[0]
	}

	siteURL := envFirst("JIRA_SITE_URL", "ATLASSIAN_SITE_URL")
	email := envFirst("JIRA_EMAIL", "ATLASSIAN_USER")
	apiToken := envFirst("JIRA_API_TOKEN", "ATLASSIAN_TOKEN")

	if siteURL == "" || email == "" || apiToken == "" {
		log.Fatal("Set ATLASSIAN_SITE_URL, ATLASSIAN_USER, and ATLASSIAN_TOKEN (or JIRA_* equivalents)")
	}

	c, err := client.New(siteURL, email, apiToken, "", "", nil)
	if err != nil {
		log.Fatalf("creating client: %v", err)
	}

	// Single-rule mode: --id or --url.
	if ruleID != "" {
		importSingleRule(c, ruleID, outDir)
		return
	}

	// Bulk mode: list all rules, optionally filter by --label.
	importAllRules(c, labelFilter, outDir)
}

func importSingleRule(c *client.Client, uuid, outDir string) {
	fmt.Printf("Fetching rule %s ...\n", uuid)

	rule, err := c.GetRule(uuid)
	if err != nil {
		log.Fatalf("getting rule: %v", err)
	}

	resName := sanitize(rule.Name)
	hcl := generateHCL(resName, rule)
	filename := fmt.Sprintf("rule_%s.tf", resName)
	path := filepath.Join(outDir, filename)

	if err := os.WriteFile(path, []byte(hcl), 0644); err != nil {
		log.Fatalf("writing %s: %v", filename, err)
	}

	fmt.Printf("Generated %s\n", path)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  terraform plan   # review the import\n")
	fmt.Printf("  terraform apply  # import into state\n")
	fmt.Printf("  # Then remove the import block from %s\n", filename)
}

func importAllRules(c *client.Client, labelFilter, outDir string) {
	summaries, err := c.ListRules()
	if err != nil {
		log.Fatalf("listing rules: %v", err)
	}

	fmt.Printf("Found %d rules. Fetching full details...\n", len(summaries))
	if labelFilter != "" {
		fmt.Printf("Filtering by label: %s\n", labelFilter)
	}

	// Track used resource names to handle duplicates.
	usedNames := map[string]int{}
	generated := 0

	for i, s := range summaries {
		fmt.Printf("  [%d/%d] %s ... ", i+1, len(summaries), s.Name)

		rule, err := c.GetRule(s.UUID)
		if err != nil {
			fmt.Printf("SKIP (error: %v)\n", err)
			continue
		}

		// Filter by label if --label flag is set.
		if labelFilter != "" && !hasLabel(rule.Labels, labelFilter) {
			fmt.Printf("SKIP (no label %q)\n", labelFilter)
			continue
		}

		resName := sanitize(rule.Name)
		if count, exists := usedNames[resName]; exists {
			usedNames[resName] = count + 1
			resName = fmt.Sprintf("%s_%d", resName, count+1)
		} else {
			usedNames[resName] = 1
		}

		hcl := generateHCL(resName, rule)
		filename := fmt.Sprintf("rule_%s.tf", resName)
		path := filepath.Join(outDir, filename)

		if err := os.WriteFile(path, []byte(hcl), 0644); err != nil {
			fmt.Printf("SKIP (write error: %v)\n", err)
			continue
		}

		generated++
		fmt.Printf("-> %s\n", filename)
	}

	if generated == 0 {
		fmt.Printf("\nNo rules matched.\n")
		return
	}

	fmt.Printf("\nDone. Generated %d rule files in %s\n", generated, outDir)
	fmt.Printf("Next steps:\n")
	fmt.Printf("  terraform plan   # review the imports\n")
	fmt.Printf("  terraform apply  # import into state\n")
	fmt.Printf("  # Then remove the import blocks from each rule_*.tf file\n")
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func sanitize(name string) string {
	s := strings.ToLower(name)
	s = nonAlnum.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "unnamed"
	}
	// Terraform identifiers can't start with a digit.
	if s[0] >= '0' && s[0] <= '9' {
		s = "r_" + s
	}
	return s
}

func generateImportBlock(resName, uuid string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "import {\n")
	fmt.Fprintf(&b, "  to = jira-automation_rule.%s\n", resName)
	fmt.Fprintf(&b, "  id = %q\n", uuid)
	fmt.Fprintf(&b, "}\n")
	return b.String()
}

func generateHCL(resName string, rule *client.Rule) string {
	var b strings.Builder

	enabled := rule.State == "ENABLED"

	// Import block — remove after first terraform apply.
	b.WriteString(generateImportBlock(resName, rule.UUID))
	b.WriteString("\n")

	fmt.Fprintf(&b, "resource \"jira-automation_rule\" %q {\n", resName)
	fmt.Fprintf(&b, "  name    = %q\n", rule.Name)
	fmt.Fprintf(&b, "  enabled = %v\n", enabled)

	// scope is computed-only (assigned by the API), not emitted.
	// labels are managed via internal API, not emitted.

	// Trigger JSON
	triggerVal := parseAndStrip(rule.Trigger)
	fmt.Fprintf(&b, "\n  trigger_json = jsonencode(%s)\n", renderHCLExpr(triggerVal, 2))

	// Components JSON
	compsVal := parseAndStripArray(rule.Components)
	fmt.Fprintf(&b, "\n  components_json = jsonencode(%s)\n", renderHCLExpr(compsVal, 2))

	fmt.Fprintf(&b, "}\n")
	return b.String()
}

func parseAndStrip(raw json.RawMessage) interface{} {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	stripAPIFields(v)
	return v
}

func parseAndStripArray(raws []json.RawMessage) interface{} {
	arr := make([]interface{}, 0, len(raws))
	for _, raw := range raws {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			continue
		}
		stripAPIFields(v)
		arr = append(arr, v)
	}
	return arr
}

// renderHCLExpr renders a parsed JSON value as an HCL expression for use inside jsonencode().
// level is the indentation of the opening bracket; content is indented level+2.
func renderHCLExpr(v interface{}, level int) string {
	indent := strings.Repeat(" ", level+2)
	closingIndent := strings.Repeat(" ", level)

	switch val := v.(type) {
	case nil:
		return "null"
	case bool:
		if val {
			return "true"
		}
		return "false"
	case json.Number:
		return val.String()
	case string:
		return fmt.Sprintf("%q", val)
	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[\n")
		for _, elem := range val {
			b.WriteString(indent)
			b.WriteString(renderHCLExpr(elem, level+2))
			b.WriteString(",\n")
		}
		b.WriteString(closingIndent)
		b.WriteString("]")
		return b.String()
	case map[string]interface{}:
		if len(val) == 0 {
			return "{}"
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("{\n")
		for _, k := range keys {
			b.WriteString(indent)
			b.WriteString(k)
			b.WriteString(" = ")
			b.WriteString(renderHCLExpr(val[k], level+2))
			b.WriteString("\n")
		}
		b.WriteString(closingIndent)
		b.WriteString("}")
		return b.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// stripAPIFields recursively removes API-assigned fields from component JSON.
func stripAPIFields(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}

	delete(m, "id")
	delete(m, "parentId")
	delete(m, "conditionParentId")

	if children, ok := m["children"].([]interface{}); ok {
		for _, child := range children {
			stripAPIFields(child)
		}
	}
	if conditions, ok := m["conditions"].([]interface{}); ok {
		for _, cond := range conditions {
			stripAPIFields(cond)
		}
	}
}
