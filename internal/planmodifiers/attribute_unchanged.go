package planmodifiers

import (
	"context"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

// This modifier has a similar behavior to `UseStateForUnknown`.
// The state value will be used as the plan value unless the specified attribute has changed.
// The generic type should be the Terraform type for the attribute referenced by the path.
func UseStateForUnknownIfAttributeUnchanged[T any](attribute path.Path) interface {
	planmodifier.String
	planmodifier.List
} {
	return useStateForUnknownIfAttributeUnchangedModifier[T]{attribute: attribute}
}

// useStateForUnknownIfAttributeUnchangedModifier implements the plan modifier.
type useStateForUnknownIfAttributeUnchangedModifier[T any] struct {
	attribute path.Path
}

func (m useStateForUnknownIfAttributeUnchangedModifier[T]) Description(_ context.Context) string {
	return "Once set, the value of this attribute in state will not change unless the specified attribute changes."
}

func (m useStateForUnknownIfAttributeUnchangedModifier[T]) MarkdownDescription(_ context.Context) string {
	return "Once set, the value of this attribute in state will not change unless the specified attribute changes."
}

// Returns whether the attribute referenced by the `useStateForUnknownIfAttributeUnchangedModifier` has changed between
// the given state and plan.
func (m useStateForUnknownIfAttributeUnchangedModifier[T]) hasAttributeChanged(ctx context.Context, state tfsdk.State, plan tfsdk.Plan) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	var stateValue T
	diags.Append(state.GetAttribute(ctx, m.attribute, &stateValue)...)
	if diags.HasError() {
		return false, diags
	}

	var planValue T
	diags.Append(plan.GetAttribute(ctx, m.attribute, &planValue)...)
	if diags.HasError() {
		return false, diags
	}

	hasChanged := !reflect.DeepEqual(planValue, stateValue)

	return hasChanged, diags
}

func (m useStateForUnknownIfAttributeUnchangedModifier[T]) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Do nothing if there is no state value, if there is a known planned value, or if there is an unknown configuration
	// value, otherwise interpolation gets messed up.
	if req.StateValue.IsNull() || !req.PlanValue.IsUnknown() || req.ConfigValue.IsUnknown() {
		return
	}

	hasChanged, diags := m.hasAttributeChanged(ctx, req.State, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If the attribute has not changed, the plan value can be marked as known.
	if !hasChanged {
		resp.PlanValue = req.StateValue
	}
}

func (m useStateForUnknownIfAttributeUnchangedModifier[T]) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	// Do nothing if there is no state value, if there is a known planned value, or if there is an unknown configuration
	// value, otherwise interpolation gets messed up.
	if req.StateValue.IsNull() || !req.PlanValue.IsUnknown() || req.ConfigValue.IsUnknown() {
		return
	}

	hasChanged, diags := m.hasAttributeChanged(ctx, req.State, req.Plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If the attribute has not changed, the plan value can be marked as known.
	if !hasChanged {
		resp.PlanValue = req.StateValue
	}
}
