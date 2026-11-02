package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// captureGolden fetches the raw API JSON for a rule and writes/compares it against
// a golden file in testdata/golden/<name>.json.
//
// Behaviour is controlled by environment variables:
//   - GOLDEN_UPDATE=1: write the golden file (create or overwrite)
//   - GOLDEN_STRICT=1: fail the test if the golden file differs (CI mode)
//   - Otherwise: log a warning on diff
func captureGolden(t *testing.T, name string, resourceAddr string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceAddr]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceAddr)
		}

		c, err := testAccNewClient()
		if err != nil {
			return fmt.Errorf("creating client for golden capture: %w", err)
		}

		raw, err := c.GetRuleRaw(rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("fetching rule JSON for golden capture: %w", err)
		}

		// Strip volatile fields before comparison.
		cleaned := stripVolatileFields(raw)

		pretty, err := json.MarshalIndent(cleaned, "", "  ")
		if err != nil {
			return fmt.Errorf("formatting golden JSON: %w", err)
		}

		goldenPath := filepath.Join(repoRoot(), "testdata", "golden", name+".json")

		if os.Getenv("GOLDEN_UPDATE") == "1" {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
				return fmt.Errorf("creating golden directory: %w", err)
			}
			if err := os.WriteFile(goldenPath, append(pretty, '\n'), 0o644); err != nil {
				return fmt.Errorf("writing golden file: %w", err)
			}
			t.Logf("golden file updated: %s", goldenPath)
			return nil
		}

		existing, err := os.ReadFile(goldenPath)
		if err != nil {
			if os.IsNotExist(err) {
				t.Logf("golden file missing: %s (run with GOLDEN_UPDATE=1 to create)", goldenPath)
				return nil
			}
			return fmt.Errorf("reading golden file: %w", err)
		}

		if string(existing) != string(append(pretty, '\n')) {
			msg := fmt.Sprintf("golden file differs: %s (run with GOLDEN_UPDATE=1 to update)", goldenPath)
			if os.Getenv("GOLDEN_STRICT") == "1" {
				return fmt.Errorf("%s", msg)
			}
			t.Log(msg)
		}

		return nil
	}
}

// stripVolatileFields removes fields that change between runs (IDs, timestamps, author).
func stripVolatileFields(raw json.RawMessage) interface{} {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}

	volatileKeys := []string{"id", "created", "updated", "authorAccountId", "clientKey", "ruleKey"}
	for _, key := range volatileKeys {
		delete(obj, key)
	}

	return obj
}

// --- Doc example acceptance tests ---

func TestAccDocExample_simpleHCL(t *testing.T) {
	replacements := defaultReplacements("tf-acc-doc-simple-hcl", `"Log on transition"`)
	config := loadExample(t, "simple_hcl.tf", replacements)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.log_on_transition", "id"),
					resource.TestCheckResourceAttr("jira-automation_rule.log_on_transition", "name", "tf-acc-doc-simple-hcl"),
					captureGolden(t, "simple_hcl", "jira-automation_rule.log_on_transition"),
				),
			},
		},
	})
}

func TestAccDocExample_debugWebhook(t *testing.T) {
	replacements := defaultReplacements("tf-acc-doc-debug-webhook", `"Add release related work"`)
	config := loadExample(t, "debug_webhook.tf", replacements)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheckWithWebhook(t)
			testAccPreCheckWithProjectID(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.release_work", "id"),
					resource.TestCheckResourceAttr("jira-automation_rule.release_work", "name", "tf-acc-doc-debug-webhook"),
					captureGolden(t, "debug_webhook", "jira-automation_rule.release_work"),
				),
			},
		},
	})
}

func TestAccDocExample_conditionThenElse(t *testing.T) {
	replacements := defaultReplacements("tf-acc-doc-condition", `"Comment on high-priority issues"`)
	config := loadExample(t, "condition_then_else.tf", replacements)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.conditional_comment", "id"),
					resource.TestCheckResourceAttr("jira-automation_rule.conditional_comment", "name", "tf-acc-doc-condition"),
					resource.TestCheckResourceAttr("jira-automation_rule.conditional_comment", "components.0.type", "condition"),
					captureGolden(t, "condition_then_else", "jira-automation_rule.conditional_comment"),
				),
			},
		},
	})
}

func TestAccDocExample_rawJSON(t *testing.T) {
	replacements := defaultReplacements("tf-acc-doc-raw-json", `"My Rule"`)
	config := loadExample(t, "raw_json.tf", replacements)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.json_fallback", "id"),
					resource.TestCheckResourceAttr("jira-automation_rule.json_fallback", "name", "tf-acc-doc-raw-json"),
					captureGolden(t, "raw_json", "jira-automation_rule.json_fallback"),
				),
			},
		},
	})
}
