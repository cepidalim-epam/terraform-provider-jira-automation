package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"terraform-provider-jira-automation/internal/client"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ruleResource{}
	_ resource.ResourceWithImportState = &ruleResource{}
)

type ruleResource struct {
	client *client.Client
}

type ruleResourceModel struct {
	ID             types.String         `tfsdk:"id"`
	Name           types.String         `tfsdk:"name"`
	Enabled        types.Bool           `tfsdk:"enabled"`
	State          types.String         `tfsdk:"state"`
	Scope          types.List           `tfsdk:"scope"`
	Labels         types.List           `tfsdk:"labels"`
	ProjectID      types.String         `tfsdk:"project_id"`
	Trigger        *triggerModel        `tfsdk:"trigger"`
	TriggerJSON    jsontypes.Normalized `tfsdk:"trigger_json"`
	Components     []componentModel     `tfsdk:"components"`
	ComponentsJSON jsontypes.Normalized `tfsdk:"components_json"`
}

func NewRuleResource() resource.Resource {
	return &ruleResource{}
}

func (r *ruleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rule"
}

func (r *ruleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Jira Automation rule. Note: the public API has no DELETE endpoint, so terraform destroy will disable the rule instead of deleting it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Rule UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Rule name.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the rule is enabled.",
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "Rule state (ENABLED or DISABLED).",
				PlanModifiers: []planmodifier.String{
					stateFromEnabledModifier{},
				},
			},
			"scope": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Rule scope ARIs assigned by the API.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"labels": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Rule labels (read-only). The provider auto-tags rules with managed-by:terraform but labels cannot be set via config. Use the Jira UI to manage labels.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "Jira project numeric ID. Used to scope event-based triggers to a project.",
			},
			"trigger": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Structured trigger configuration. Mutually exclusive with trigger_json.",
				Validators: []validator.Object{
					objectvalidator.ExactlyOneOf(path.MatchRoot("trigger_json")),
				},
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:    true,
						Description: "Trigger type (e.g. status_transition).",
					},
					"args": schema.MapAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Trigger arguments as key-value pairs.",
					},
				},
			},
			"trigger_json": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Trigger configuration as a JSON string. Mutually exclusive with trigger.",
			},
			"components": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Structured component configuration. Mutually exclusive with components_json.",
				Validators: []validator.List{
					listvalidator.ExactlyOneOf(path.MatchRoot("components_json")),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Required:    true,
							Description: "Component type (e.g. condition, log, comment, add_release_related_work).",
						},
						"args": schema.MapAttribute{
							Optional:    true,
							ElementType: types.StringType,
							Description: "Component arguments as key-value pairs.",
						},
						"then": schema.ListNestedAttribute{
							Optional:    true,
							Description: "Actions to execute when the condition is true.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"type": schema.StringAttribute{
										Required:    true,
										Description: "Action type.",
									},
									"args": schema.MapAttribute{
										Optional:    true,
										ElementType: types.StringType,
										Description: "Action arguments as key-value pairs.",
									},
								},
							},
						},
						"else": schema.ListNestedAttribute{
							Optional:    true,
							Description: "Actions to execute when the condition is false.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"type": schema.StringAttribute{
										Required:    true,
										Description: "Action type.",
									},
									"args": schema.MapAttribute{
										Optional:    true,
										ElementType: types.StringType,
										Description: "Action arguments as key-value pairs.",
									},
								},
							},
						},
					},
				},
			},
			"components_json": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Components (actions/conditions) as a JSON array string. Mutually exclusive with components.",
			},
		},
	}
}

func (r *ruleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *ruleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ruleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	trigger, diags := r.resolveTriggerJSON(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	components, d := r.resolveComponentsJSON(ctx, &plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.CreateRuleRequest{
		Name:       plan.Name.ValueString(),
		ProjectID:  plan.ProjectID.ValueString(),
		Trigger:    trigger,
		Components: components,
	}

	uuid, err := r.client.CreateRule(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating rule", err.Error())
		return
	}

	// Set the rule state after creation if needed.
	enabled := plan.Enabled.ValueBool()
	if err := r.client.SetRuleState(uuid, enabled); err != nil {
		resp.Diagnostics.AddError("Error setting rule state after creation", err.Error())
		return
	}

	// Read back the created rule to populate computed fields (scope, state, etc.).
	diags = r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Tag with managed-by:terraform.
	r.syncManagedLabel(ctx, uuid, plan, &resp.Diagnostics)

	// Re-read to pick up the label.
	diags = r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ruleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ruleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags := r.readIntoModel(ctx, state.ID.ValueString(), &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ruleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ruleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ruleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	uuid := state.ID.ValueString()

	trigger, d := r.resolveTriggerJSON(ctx, &plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	components, cd := r.resolveComponentsJSON(ctx, &plan)
	resp.Diagnostics.Append(cd...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := client.UpdateRuleRequest{
		Name:       plan.Name.ValueString(),
		Trigger:    trigger,
		Components: components,
	}

	if err := r.client.UpdateRule(uuid, updateReq); err != nil {
		resp.Diagnostics.AddError("Error updating rule", err.Error())
		return
	}

	// Handle enabled state change.
	enabled := plan.Enabled.ValueBool()
	if err := r.client.SetRuleState(uuid, enabled); err != nil {
		resp.Diagnostics.AddError("Error setting rule state", err.Error())
		return
	}

	// Read back the updated rule.
	diags := r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Tag with managed-by:terraform.
	r.syncManagedLabel(ctx, uuid, plan, &resp.Diagnostics)

	// Re-read to pick up the label.
	diags = r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ruleResource) Delete(_ context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ruleResourceModel
	resp.Diagnostics.Append(req.State.Get(context.Background(), &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No DELETE endpoint in the public API — disable the rule instead.
	uuid := state.ID.ValueString()
	if err := r.client.SetRuleState(uuid, false); err != nil {
		resp.Diagnostics.AddError("Error disabling rule on destroy",
			fmt.Sprintf("The Jira Automation API has no DELETE endpoint. Attempted to disable rule %s instead, but got error: %s", uuid, err.Error()))
		return
	}

	resp.Diagnostics.AddWarning("Rule disabled, not deleted",
		fmt.Sprintf("Rule %s was disabled because the Jira Automation API does not support deletion. You may want to manually remove it from the Jira UI.", uuid))
}

func (r *ruleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	uuid := req.ID

	var model ruleResourceModel
	diags := r.readIntoModel(ctx, uuid, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// readIntoModel fetches a rule by UUID and populates the model.
func (r *ruleResource) readIntoModel(ctx context.Context, uuid string, model *ruleResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	rule, err := r.client.GetRule(uuid)
	if err != nil {
		diags.AddError("Error reading rule", err.Error())
		return diags
	}

	model.ID = types.StringValue(rule.UUID)
	model.Name = types.StringValue(rule.Name)
	model.State = types.StringValue(rule.State)
	model.Enabled = types.BoolValue(rule.State == "ENABLED")

	// Scope
	if len(rule.RuleScopeARIs) > 0 {
		scopeList, d := types.ListValueFrom(ctx, types.StringType, rule.RuleScopeARIs)
		diags.Append(d...)
		model.Scope = scopeList
	} else {
		model.Scope = types.ListNull(types.StringType)
	}

	// Labels — read-only, show whatever the API returns.
	if len(rule.Labels) > 0 {
		labelList, d := types.ListValueFrom(ctx, types.StringType, rule.Labels)
		diags.Append(d...)
		model.Labels = labelList
	} else {
		model.Labels = types.ListNull(types.StringType)
	}

	// Trigger — if the user used the structured trigger block, parse the API
	// response back into the trigger model. Otherwise, populate trigger_json.
	if model.Trigger != nil {
		triggerType, args, err := ParseTrigger(rule.Trigger)
		if err != nil {
			diags.AddError("Error parsing trigger from API", err.Error())
			return diags
		}
		argsMap, d := types.MapValueFrom(ctx, types.StringType, args)
		diags.Append(d...)
		model.Trigger = &triggerModel{
			Type: types.StringValue(triggerType),
			Args: argsMap,
		}
	} else {
		triggerNorm, err := normalizeRawJSON(rule.Trigger)
		if err != nil {
			diags.AddError("Error normalizing trigger", err.Error())
			return diags
		}
		model.TriggerJSON = jsontypes.NewNormalizedValue(triggerNorm)
	}

	// Components — if the user used the structured components block, parse the API
	// response back into component models. Otherwise, populate components_json.
	if model.Components != nil {
		parsed, err := ParseComponents(rule.Components, ctx, r.client.ReverseAliases)
		if err != nil {
			diags.AddError("Error parsing components from API", err.Error())
			return diags
		}
		model.Components = parsed
	} else {
		componentsNorm, err := normalizeRawJSONArray(rule.Components)
		if err != nil {
			diags.AddError("Error normalizing components", err.Error())
			return diags
		}
		model.ComponentsJSON = jsontypes.NewNormalizedValue(componentsNorm)
	}

	return diags
}

// resolveTriggerJSON returns the trigger JSON from either the structured trigger
// block or the raw trigger_json attribute.
func (r *ruleResource) resolveTriggerJSON(ctx context.Context, model *ruleResourceModel) (json.RawMessage, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Trigger != nil {
		triggerType := model.Trigger.Type.ValueString()

		args := make(map[string]string)
		if !model.Trigger.Args.IsNull() && !model.Trigger.Args.IsUnknown() {
			diags.Append(model.Trigger.Args.ElementsAs(ctx, &args, false)...)
			if diags.HasError() {
				return nil, diags
			}
		}

		projectID := model.ProjectID.ValueString()
		raw, err := BuildTriggerJSON(triggerType, args, r.client.CloudID, projectID)
		if err != nil {
			diags.AddError("Error building trigger JSON", err.Error())
			return nil, diags
		}
		return raw, diags
	}

	return json.RawMessage(model.TriggerJSON.ValueString()), diags
}

// resolveComponentsJSON returns the components JSON from either the structured
// components block or the raw components_json attribute.
func (r *ruleResource) resolveComponentsJSON(ctx context.Context, model *ruleResourceModel) ([]json.RawMessage, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Components != nil {
		raws, err := BuildComponentsJSON(model.Components, r.client.CloudID, r.client.WebhookUser, r.client.WebhookToken, ctx, r.client.FieldAliases)
		if err != nil {
			diags.AddError("Error building components JSON", err.Error())
			return nil, diags
		}
		return raws, diags
	}

	components, err := parseComponentsJSON(model.ComponentsJSON.ValueString())
	if err != nil {
		diags.AddError("Invalid components_json", err.Error())
		return nil, diags
	}
	return components, diags
}

// Helper functions

func parseComponentsJSON(s string) ([]json.RawMessage, error) {
	var components []json.RawMessage
	if err := json.Unmarshal([]byte(s), &components); err != nil {
		return nil, fmt.Errorf("invalid components_json: %w", err)
	}
	return components, nil
}

// normalizeRawJSON round-trips raw JSON through interface{} for canonical output,
// stripping API-assigned fields (id, parentId, conditionParentId) that aren't
// part of the Terraform config.
func normalizeRawJSON(raw json.RawMessage) (string, error) {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	stripAPIFields(v)
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func normalizeRawJSONArray(raws []json.RawMessage) (string, error) {
	var arr []interface{}
	for _, raw := range raws {
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", err
		}
		stripAPIFields(v)
		arr = append(arr, v)
	}
	out, err := json.Marshal(arr)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// stripAPIFields recursively removes API-assigned/enriched fields from JSON
// so the normalized output matches the Terraform config (which doesn't include them).
func stripAPIFields(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}

	// Remove structural IDs and computed fields.
	delete(m, "id")
	delete(m, "parentId")
	delete(m, "conditionParentId")
	delete(m, "connectionId")

	// Remove empty containers the API always adds.
	if children, ok := m["children"].([]interface{}); ok {
		if len(children) == 0 {
			delete(m, "children")
		} else {
			for _, child := range children {
				stripAPIFields(child)
			}
		}
	}
	if conditions, ok := m["conditions"].([]interface{}); ok {
		if len(conditions) == 0 {
			delete(m, "conditions")
		} else {
			for _, cond := range conditions {
				stripAPIFields(cond)
			}
		}
	}

	// Remove API-enriched fields from trigger/component values.
	// The API adds eventFilters, eventKey, issueEvent to trigger values.
	if val, ok := m["value"].(map[string]interface{}); ok {
		delete(val, "eventFilters")
		delete(val, "eventKey")
		delete(val, "issueEvent")
	}
}

func toStringSlice(ctx context.Context, list types.List) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var strs []string
	list.ElementsAs(ctx, &strs, false)
	return strs
}

// syncManagedLabel tags the rule with managed-by:terraform via the internal API.
// Warns instead of failing if the label doesn't exist — the user must create it in the Jira UI.
func (r *ruleResource) syncManagedLabel(ctx context.Context, uuid string, model ruleResourceModel, diags *diag.Diagnostics) {
	scopes := toStringSlice(ctx, model.Scope)
	if len(scopes) != 1 {
		return // Global or multi-project — skip.
	}
	projectID := client.ExtractProjectID(scopes[0])
	if projectID == "" {
		return
	}

	// Look up the managed-by:terraform label. If it doesn't exist, warn the user.
	labels, err := r.client.ListLabels(projectID)
	if err != nil {
		diags.AddWarning("Could not list labels",
			fmt.Sprintf("Could not list labels for project %s: %s. Create a 'managed-by:terraform' label in the Jira UI to tag managed rules.", projectID, err))
		return
	}

	var labelID int
	for _, l := range labels {
		if l.Name == "managed-by:terraform" {
			labelID = l.ID
			break
		}
	}

	if labelID == 0 {
		diags.AddWarning("Label 'managed-by:terraform' not found",
			"Create a label named 'managed-by:terraform' in the Jira Automation UI to tag Terraform-managed rules. "+
				"Go to Project Settings → Automation → Labels to create it.")
		return
	}

	if err := r.client.AddLabelToRule(projectID, uuid, labelID); err != nil {
		diags.AddWarning("Could not tag rule with managed-by:terraform",
			fmt.Sprintf("Failed to add managed-by:terraform label to rule %s: %s", uuid, err))
	}
}

// --- State plan modifier (derived from enabled) ---

type stateFromEnabledModifier struct{}

func (m stateFromEnabledModifier) Description(_ context.Context) string {
	return "Computes state from the enabled attribute."
}

func (m stateFromEnabledModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m stateFromEnabledModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Read the planned enabled value.
	var enabled types.Bool
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("enabled"), &enabled)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if enabled.IsUnknown() || enabled.IsNull() {
		// Fall back to prior state if available.
		if !req.StateValue.IsNull() {
			resp.PlanValue = req.StateValue
		}
		return
	}

	if enabled.ValueBool() {
		resp.PlanValue = types.StringValue("ENABLED")
	} else {
		resp.PlanValue = types.StringValue("DISABLED")
	}
}

