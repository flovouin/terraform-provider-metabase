package provider

import (
	"context"
	"fmt"

	"github.com/flovouin/terraform-provider-metabase/internal/graphlock"
	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// The data passed by the provider to its resources. A single instance is created for each provider configuration, such
// that all the resources sending requests to the same Metabase instance share the same trackers.
type metabaseResourceData struct {
	// The Metabase API client.
	client *metabase.ClientWithResponses

	// Serializes the requests recording a new revision of the collection graph.
	collectionGraph *graphlock.CollectionGraphTracker

	// Serializes the updates of the data permissions graph.
	permissionsGraph *graphlock.PermissionsGraphTracker
}

// A resource that can be used as the base for any Metabase resource. It references a client to make requests to the
// Metabase API.
type MetabaseBaseResource struct {
	// The name of the resource, as exposed to the Terraform API (by prefixing it with the provider name).
	name string

	// The data passed by the provider, set when the resource is configured.
	metabaseResourceData
}

func (r *MetabaseBaseResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = fmt.Sprintf("%s_%s", req.ProviderTypeName, r.name)
}

func (r *MetabaseBaseResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(*metabaseResourceData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type when configuring Metabase resource.",
			fmt.Sprintf("Expected *metabaseResourceData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.metabaseResourceData = *data
}
