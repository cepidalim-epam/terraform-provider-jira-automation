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

	siteURL := os.Getenv("JIRA_SITE_URL")
	if siteURL == "" {
		siteURL = os.Getenv("ATLASSIAN_SITE_URL")
	}
	if siteURL == "" {
		t.Fatal("JIRA_SITE_URL or ATLASSIAN_SITE_URL must be set for acceptance tests")
	}

	email := os.Getenv("JIRA_EMAIL")
	if email == "" {
		email = os.Getenv("ATLASSIAN_USER")
	}
	if email == "" {
		t.Fatal("JIRA_EMAIL or ATLASSIAN_USER must be set for acceptance tests")
	}

	token := os.Getenv("JIRA_API_TOKEN")
	if token == "" {
		token = os.Getenv("ATLASSIAN_TOKEN")
	}
	if token == "" {
		t.Fatal("JIRA_API_TOKEN or ATLASSIAN_TOKEN must be set for acceptance tests")
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

	if os.Getenv("JIRA_TEST_PROJECT_ID") == "" {
		t.Fatal("JIRA_TEST_PROJECT_ID must be set for project-scoped acceptance tests")
	}
}

// testAccNewClient creates a throwaway API client for CheckDestroy and other
// out-of-band verification during acceptance tests.
func testAccNewClient() (*client.Client, error) {
	siteURL := os.Getenv("JIRA_SITE_URL")
	if siteURL == "" {
		siteURL = os.Getenv("ATLASSIAN_SITE_URL")
	}
	email := os.Getenv("JIRA_EMAIL")
	if email == "" {
		email = os.Getenv("ATLASSIAN_USER")
	}
	token := os.Getenv("JIRA_API_TOKEN")
	if token == "" {
		token = os.Getenv("ATLASSIAN_TOKEN")
	}
	webhookUser := os.Getenv("JIRA_WEBHOOK_USER")
	webhookToken := os.Getenv("JIRA_WEBHOOK_TOKEN")

	return client.New(siteURL, email, token, webhookUser, webhookToken, nil)
}
