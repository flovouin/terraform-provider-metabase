package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// A resource that can be used as the base for any Metabase resource. It references a client to make requests to the
// Metabase API.
type MetabaseBaseResource struct {
	// The name of the resource, as exposed to the Terraform API (by prefixing it with the provider name).
	name string

	// The Metabase API client.
	client *metabase.ClientWithResponses
}

func (r *MetabaseBaseResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = fmt.Sprintf("%s_%s", req.ProviderTypeName, r.name)
}

func (r *MetabaseBaseResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*metabase.ClientWithResponses)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected client type when configuring Metabase resource.",
			fmt.Sprintf("Expected *metabase.ClientWithResponses, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}
