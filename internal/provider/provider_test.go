package provider

import (
	"os"
	"testing"

	"terraform-provider-jira-automation/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories are used to instantiate a provider during
// acceptance testing. The factory function is invoked for every Terraform CLI
// command executed to create a provider server to which the CLI can reattach.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"jira-automation": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck validates that the required environment variables for
// acceptance tests are set. Call this in the PreCheck field of resource.TestCase.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	siteURL := firstEnv("jira_url", "JIRA_URL", "JIRA_SITE_URL", "ATLASSIAN_SITE_URL")
	if siteURL == "" {
		t.Fatal("jira_url, JIRA_URL, JIRA_SITE_URL, or ATLASSIAN_SITE_URL must be set for acceptance tests")
	}

	email := firstEnv("jira_username", "JIRA_USERNAME", "JIRA_EMAIL", "ATLASSIAN_USER")
	if email == "" {
		t.Fatal("jira_username, JIRA_USERNAME, JIRA_EMAIL, or ATLASSIAN_USER must be set for acceptance tests")
	}

	token := firstEnv("jira_pat_rw", "JIRA_PAT_RW", "JIRA_API_TOKEN", "ATLASSIAN_TOKEN")
	if token == "" {
		t.Fatal("jira_pat_rw, JIRA_PAT_RW, JIRA_API_TOKEN, or ATLASSIAN_TOKEN must be set for acceptance tests")
	}
}

// testAccPreCheckWithWebhook additionally requires webhook credentials.
func testAccPreCheckWithWebhook(t *testing.T) {
	t.Helper()
	testAccPreCheck(t)

	if os.Getenv("JIRA_WEBHOOK_USER") == "" {
		t.Fatal("JIRA_WEBHOOK_USER must be set for webhook acceptance tests")
	}
	if os.Getenv("JIRA_WEBHOOK_TOKEN") == "" {
		t.Fatal("JIRA_WEBHOOK_TOKEN must be set for webhook acceptance tests")
	}
}

// testAccPreCheckWithProjectID additionally requires a test project ID.
func testAccPreCheckWithProjectID(t *testing.T) {
	t.Helper()
	testAccPreCheck(t)

	if firstEnv("jira_id", "JIRA_ID", "JIRA_TEST_PROJECT_ID") == "" {
		t.Fatal("jira_id, JIRA_ID, or JIRA_TEST_PROJECT_ID must be set for project-scoped acceptance tests")
	}
}

// testAccNewClient creates a throwaway API client for CheckDestroy and other
// out-of-band verification during acceptance tests.
func testAccNewClient() (*client.Client, error) {
	siteURL := firstEnv("jira_url", "JIRA_URL", "JIRA_SITE_URL", "ATLASSIAN_SITE_URL")
	email := firstEnv("jira_username", "JIRA_USERNAME", "JIRA_EMAIL", "ATLASSIAN_USER")
	token := firstEnv("jira_pat_rw", "JIRA_PAT_RW", "JIRA_API_TOKEN", "ATLASSIAN_TOKEN")
	webhookUser := os.Getenv("JIRA_WEBHOOK_USER")
	webhookToken := os.Getenv("JIRA_WEBHOOK_TOKEN")

	c, err := client.New(siteURL, email, token, webhookUser, webhookToken, nil)
	if err != nil {
		return nil, err
	}
	c.ProjectID = firstEnv("jira_id", "JIRA_ID", "JIRA_TEST_PROJECT_ID")
	return c, nil
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
