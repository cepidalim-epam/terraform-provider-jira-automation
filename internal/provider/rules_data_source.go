package provider

import (
	"context"
	"fmt"

	"terraform-provider-jira-automation/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &rulesDataSource{}

type rulesDataSource struct {
	client *client.Client
}

type rulesDataSourceModel struct {
	Rules []ruleSummaryModel `tfsdk:"rules"`
}

type ruleSummaryModel struct {
	UUID    types.String `tfsdk:"uuid"`
	Name    types.String `tfsdk:"name"`
	State   types.String `tfsdk:"state"`
	Enabled types.Bool   `tfsdk:"enabled"`
}

func NewRulesDataSource() datasource.DataSource {
	return &rulesDataSource{}
}

func (d *rulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rules"
}

func (d *rulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all Jira Automation rule summaries.",
		Attributes: map[string]schema.Attribute{
			"rules": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of automation rule summaries.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"uuid": schema.StringAttribute{
							Computed:    true,
							Description: "Rule UUID.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Rule name.",
						},
						"state": schema.StringAttribute{
							Computed:    true,
							Description: "Rule state (ENABLED or DISABLED).",
						},
						"enabled": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the rule is enabled.",
						},
					},
				},
			},
		},
	}
}

func (d *rulesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *rulesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	rules, err := d.client.ListRules()
	if err != nil {
		resp.Diagnostics.AddError("Unable to list rules", err.Error())
		return
	}

	var state rulesDataSourceModel
	for _, r := range rules {
		state.Rules = append(state.Rules, ruleSummaryModel{
			UUID:    types.StringValue(r.UUID),
			Name:    types.StringValue(r.Name),
			State:   types.StringValue(r.State),
			Enabled: types.BoolValue(r.Enabled),
		})
	}

	// Ensure rules is never null, always an empty list at minimum.
	if state.Rules == nil {
		state.Rules = []ruleSummaryModel{}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
