package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccRulesDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheckWithProjectID(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRulesDataSourceConfig_basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.jira-automation_rules.all", "rules.#"),
				),
			},
		},
	})
}

func testAccRulesDataSourceConfig_basic() string {
	return fmt.Sprintf(`
# Create a rule so the data source has something to find.
resource "jira-automation_rule" "dep" {
  name       = "tf-acc-datasource-dep"
  project_id = %[1]q

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
      message = "tf-acc-test: datasource dependency"
    }
  }]
}

data "jira-automation_rules" "all" {
  depends_on = [jira-automation_rule.dep]
}
`, os.Getenv("JIRA_TEST_PROJECT_ID"))
}
