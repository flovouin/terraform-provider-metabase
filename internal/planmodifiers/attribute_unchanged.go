package planmodifiers

import (
	"context"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// This modifier has a similar behavior to `UseStateForUnknown`.
// The state value will be used as the plan value unless the specified attribute has changed.
// The generic type should be the Terraform type for the attribute referenced by the path.
func UseStateForUnknownIfAttributeUnchanged[T any](attribute path.Path) planmodifier.String {
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

func (m useStateForUnknownIfAttributeUnchangedModifier[T]) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Do nothing if there is no state value.
	if req.StateValue.IsNull() {
		return
	}

	// Do nothing if there is a known planned value.
	if !req.PlanValue.IsUnknown() {
		return
	}

	// Do nothing if there is an unknown configuration value, otherwise interpolation gets messed up.
	if req.ConfigValue.IsUnknown() {
		return
	}

	// Checks whether the attribute has changed.
	var stateValue T
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, m.attribute, &stateValue)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var planValue T
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, m.attribute, &planValue)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If the attribute has not changed, the plan value can be marked as known.
	if reflect.DeepEqual(planValue, stateValue) {
		resp.PlanValue = req.StateValue
	}
}
