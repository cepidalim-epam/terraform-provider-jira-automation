package provider

import (
	"context"
	"os"
	"strings"

	"terraform-provider-jira-automation/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &jiraAutomationProvider{}

type jiraAutomationProvider struct {
	version string
}

type jiraAutomationProviderModel struct {
	SiteURL      types.String `tfsdk:"site_url"`
	Email        types.String `tfsdk:"email"`
	APIToken     types.String `tfsdk:"api_token"`
	ProjectID    types.String `tfsdk:"project_id"`
	WebhookUser  types.String `tfsdk:"webhook_user"`
	WebhookToken types.String `tfsdk:"webhook_token"`
	FieldAliases types.Map    `tfsdk:"field_aliases"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &jiraAutomationProvider{version: version}
	}
}

func (p *jiraAutomationProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "jira-automation"
	resp.Version = p.version
}

func (p *jiraAutomationProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for managing Jira Automation rules.",
		Attributes: map[string]schema.Attribute{
			"site_url": schema.StringAttribute{
				Description: "The Jira site URL. Can also be set via jira_url, JIRA_URL, or legacy JIRA_SITE_URL / ATLASSIAN_SITE_URL environment variables.",
				Optional:    true,
			},
			"email": schema.StringAttribute{
				Description: "The Jira username for API authentication. Can also be set via jira_username, JIRA_USERNAME, or legacy JIRA_EMAIL / ATLASSIAN_USER environment variables.",
				Optional:    true,
			},
			"api_token": schema.StringAttribute{
				Description: "The Jira PAT for API authentication. Can also be set via jira_pat_rw, JIRA_PAT_RW, or legacy JIRA_API_TOKEN / ATLASSIAN_TOKEN environment variables.",
				Optional:    true,
				Sensitive:   true,
			},
			"project_id": schema.StringAttribute{
				Description: "Numeric Jira project ID for Jira Data Center Automation rule operations. Can also be set via jira_id or JIRA_ID environment variables.",
				Optional:    true,
			},
			"webhook_user": schema.StringAttribute{
				Description: "Email for outgoing webhook Basic auth (service account). Can also be set via JIRA_WEBHOOK_USER env var.",
				Optional:    true,
			},
			"webhook_token": schema.StringAttribute{
				Description: "API token for outgoing webhook Basic auth. Can also be set via JIRA_WEBHOOK_TOKEN env var.",
				Optional:    true,
				Sensitive:   true,
			},
			"field_aliases": schema.MapAttribute{
				Description: "Map of friendly alias names to Jira custom field IDs (e.g. release_version = \"customfield_10709\"). " +
					"Aliases can be used in smart values ({{issue.ALIAS}}) and as bare arg values; the provider resolves them to field IDs on write and reverses on read.",
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (p *jiraAutomationProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config jiraAutomationProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	siteURL := stringValueOrEnv(config.SiteURL, "jira_url", "JIRA_URL", "JIRA_SITE_URL", "ATLASSIAN_SITE_URL")
	email := stringValueOrEnv(config.Email, "jira_username", "JIRA_USERNAME", "JIRA_EMAIL", "ATLASSIAN_USER")
	apiToken := stringValueOrEnv(config.APIToken, "jira_pat_rw", "JIRA_PAT_RW", "JIRA_API_TOKEN", "ATLASSIAN_TOKEN")
	webhookUser := stringValueOrEnv(config.WebhookUser, "JIRA_WEBHOOK_USER")
	webhookToken := stringValueOrEnv(config.WebhookToken, "JIRA_WEBHOOK_TOKEN")
	projectID := stringValueOrEnv(config.ProjectID, "jira_id", "JIRA_ID", "JIRA_TEST_PROJECT_ID")
	projectID = strings.TrimSpace(projectID)

	if siteURL == "" {
		resp.Diagnostics.AddError("Missing site_url", "site_url must be set in provider config or jira_url / JIRA_URL env var.")
		return
	}
	if email == "" {
		resp.Diagnostics.AddError("Missing email", "email must be set in provider config or jira_username / JIRA_USERNAME env var.")
		return
	}
	if apiToken == "" {
		resp.Diagnostics.AddError("Missing api_token", "api_token must be set in provider config or jira_pat_rw / JIRA_PAT_RW env var.")
		return
	}
	if projectID != "" {
		parsedProjectID, err := client.ParseProjectID(projectID)
		if err != nil {
			resp.Diagnostics.AddError("Invalid project_id", err.Error())
			return
		}
		projectID = parsedProjectID
	}

	// Extract field aliases map (nil if not configured).
	var aliases map[string]string
	if !config.FieldAliases.IsNull() && !config.FieldAliases.IsUnknown() {
		aliases = make(map[string]string)
		resp.Diagnostics.Append(config.FieldAliases.ElementsAs(ctx, &aliases, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	c, err := client.New(siteURL, email, apiToken, webhookUser, webhookToken, aliases)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create API client", err.Error())
		return
	}
	if c.IsDataCenter && projectID == "" {
		resp.Diagnostics.AddError("Missing jira_id", "jira_id (numeric Jira project ID) is required for Jira Data Center Automation API operations.")
		return
	}

	c.ProjectID = projectID
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *jiraAutomationProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRuleResource,
	}
}

func (p *jiraAutomationProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewRulesDataSource,
	}
}

// stringValueOrEnv returns the Terraform config value if set, otherwise checks env vars.
func stringValueOrEnv(val types.String, envVars ...string) string {
	if !val.IsNull() && !val.IsUnknown() {
		return val.ValueString()
	}
	for _, env := range envVars {
		if env == "" {
			continue
		}
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return ""
}
