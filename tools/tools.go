//go:build tools

package tools

import (
	// Documentation generation.
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
	// OpenAPI client generation.
	_ "github.com/deepmap/oapi-codegen/cmd/oapi-codegen"
)
