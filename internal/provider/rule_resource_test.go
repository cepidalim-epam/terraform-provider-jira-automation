package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccCheckRuleResourceDestroy verifies that all test rules are DISABLED
// after terraform destroy (the API has no DELETE endpoint).
func testAccCheckRuleResourceDestroy(s *terraform.State) error {
	c, err := testAccNewClient()
	if err != nil {
		return fmt.Errorf("creating test client: %w", err)
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "jira-automation_rule" {
			continue
		}
		rule, err := c.GetRule(rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("getting rule %s: %w", rs.Primary.ID, err)
		}
		if rule.State != "DISABLED" {
			return fmt.Errorf("rule %s still %s, expected DISABLED", rs.Primary.ID, rule.State)
		}
	}
	return nil
}

// --- Phase 1: Basic CRUD ---

func TestAccRuleResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_basic("tf-acc-basic"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.test", "id"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "name", "tf-acc-basic"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "enabled", "true"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "state", "ENABLED"),
					resource.TestCheckResourceAttrSet("jira-automation_rule.test", "scope.#"),
				),
			},
		},
	})
}

func TestAccRuleResource_updateName(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_basic("tf-acc-update-name"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "name", "tf-acc-update-name"),
				),
			},
			{
				Config: testAccRuleResourceConfig_basic("tf-acc-update-name-renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "name", "tf-acc-update-name-renamed"),
				),
			},
		},
	})
}

func TestAccRuleResource_updateComponents(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_basic("tf-acc-update-components"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "components.0.type", "log"),
				),
			},
			{
				Config: testAccRuleResourceConfig_comment("tf-acc-update-components"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "components.0.type", "comment"),
				),
			},
		},
	})
}

func TestAccRuleResource_disableEnable(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_enabled("tf-acc-disable-enable", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "enabled", "true"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "state", "ENABLED"),
				),
			},
			{
				Config: testAccRuleResourceConfig_enabled("tf-acc-disable-enable", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "enabled", "false"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "state", "DISABLED"),
				),
			},
			{
				Config: testAccRuleResourceConfig_enabled("tf-acc-disable-enable", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "enabled", "true"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "state", "ENABLED"),
				),
			},
		},
	})
}

func TestAccRuleResource_destroyDisables(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_basic("tf-acc-destroy-disables"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "state", "ENABLED"),
				),
			},
		},
	})
}

func TestAccRuleResource_import(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_basic("tf-acc-import"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.test", "id"),
				),
			},
			{
				ResourceName:      "jira-automation_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Import always produces trigger_json/components_json (raw JSON),
				// not the structured trigger/components attributes, since the API
				// doesn't know the user's preferred format.
				ImportStateVerifyIgnore: []string{"project_id", "trigger", "trigger_json", "components", "components_json"},
			},
		},
	})
}

// --- Phase 2: Escape hatches & advanced ---

func TestAccRuleResource_triggerJSON(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_triggerJSON("tf-acc-trigger-json"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.test", "id"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "name", "tf-acc-trigger-json"),
				),
			},
		},
	})
}

func TestAccRuleResource_componentsJSON(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_componentsJSON("tf-acc-components-json"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("jira-automation_rule.test", "id"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "name", "tf-acc-components-json"),
				),
			},
		},
	})
}

func TestAccRuleResource_conditionThenElse(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_condition("tf-acc-condition"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "components.0.type", "condition"),
				),
			},
		},
	})
}

func TestAccRuleResource_fieldAliases(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_aliases("tf-acc-aliases"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "name", "tf-acc-aliases"),
					resource.TestCheckResourceAttr("jira-automation_rule.test", "components.0.type", "log"),
				),
			},
		},
	})
}

// --- Phase 3: Webhook components ---

func TestAccRuleResource_addReleaseRelatedWork(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithWebhook(t); testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_webhook("tf-acc-webhook", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "components.0.type", "add_release_related_work"),
				),
			},
		},
	})
}

func TestAccRuleResource_addReleaseRelatedWorkDebug(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithWebhook(t); testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRuleResourceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccRuleResourceConfig_webhook("tf-acc-webhook-debug", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jira-automation_rule.test", "components.0.type", "add_release_related_work"),
				),
			},
		},
	})
}

// --- HCL config templates ---

func testAccRuleResourceConfig_basic(name string) string {
	return fmt.Sprintf(`
resource "jira-automation_rule" "test" {
  name       = %[1]q
  project_id = %[2]q

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "log"
    args = {
      message = "tf-acc-test: %[1]s"
    }
  }]
}
`, name, os.Getenv("JIRA_TEST_PROJECT_ID"))
}

func testAccRuleResourceConfig_comment(name string) string {
	return fmt.Sprintf(`
resource "jira-automation_rule" "test" {
  name       = %[1]q
  project_id = %[2]q

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "comment"
    args = {
      message = "tf-acc-test: %[1]s"
    }
  }]
}
`, name, os.Getenv("JIRA_TEST_PROJECT_ID"))
}

func testAccRuleResourceConfig_enabled(name string, enabled bool) string {
	return fmt.Sprintf(`
resource "jira-automation_rule" "test" {
  name       = %[1]q
  enabled    = %[2]t
  project_id = %[3]q

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "log"
    args = {
      message = "tf-acc-test: %[1]s"
    }
  }]
}
`, name, enabled, os.Getenv("JIRA_TEST_PROJECT_ID"))
}

func testAccRuleResourceConfig_triggerJSON(name string) string {
	return fmt.Sprintf(`
resource "jira-automation_rule" "test" {
  name       = %[1]q
  project_id = %[2]q

  trigger_json = jsonencode({
    component     = "TRIGGER"
    schemaVersion = 1
    type          = "jira.issue.event.trigger:transitioned"
    value = {
      fromStatus = [{ type = "NAME", value = "To Do" }]
      toStatus   = [{ type = "NAME", value = "In Progress" }]
    }
  })

  components = [{
    type = "log"
    args = {
      message = "tf-acc-test: %[1]s"
    }
  }]
}
`, name, os.Getenv("JIRA_TEST_PROJECT_ID"))
}

func testAccRuleResourceConfig_componentsJSON(name string) string {
	return fmt.Sprintf(`
resource "jira-automation_rule" "test" {
  name       = %[1]q
  project_id = %[2]q

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components_json = jsonencode([
    {
      component     = "ACTION"
      schemaVersion = 1
      type          = "codebarrel.action.log"
      value         = "tf-acc-test: %[1]s via components_json"
    }
  ])
}
`, name, os.Getenv("JIRA_TEST_PROJECT_ID"))
}

func testAccRuleResourceConfig_condition(name string) string {
	return fmt.Sprintf(`
resource "jira-automation_rule" "test" {
  name       = %[1]q
  project_id = %[2]q

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "condition"
    args = {
      first    = "{{issue.status.name}}"
      operator = "equals"
      second   = "In Progress"
    }

    then = [{
      type = "log"
      args = {
        message = "tf-acc-test: condition was true"
      }
    }]

    else = [{
      type = "log"
      args = {
        message = "tf-acc-test: condition was false"
      }
    }]
  }]
}
`, name, os.Getenv("JIRA_TEST_PROJECT_ID"))
}

func testAccRuleResourceConfig_aliases(name string) string {
	return fmt.Sprintf(`
provider "jira-automation" {
  field_aliases = {
    my_status = "status"
  }
}

resource "jira-automation_rule" "test" {
  name       = %[1]q
  project_id = %[2]q

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "log"
    args = {
      message = "tf-acc-test: alias={{issue.my_status}}"
    }
  }]
}
`, name, os.Getenv("JIRA_TEST_PROJECT_ID"))
}

func testAccRuleResourceConfig_webhook(name string, debug bool) string {
	debugArg := ""
	if debug {
		debugArg = `      debug         = "true"`
	}
	return fmt.Sprintf(`
resource "jira-automation_rule" "test" {
  name       = %[1]q
  project_id = %[2]q

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "add_release_related_work"
    args = {
      version_field = "customfield_10020"
      category      = "other"
      title         = "tf-acc-test: %[1]s"
      url           = "https://example.com/tf-acc-test"
%[3]s
    }
  }]
}
`, name, os.Getenv("JIRA_TEST_PROJECT_ID"), debugArg)
}
