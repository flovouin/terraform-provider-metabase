package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

var providerConfig = fmt.Sprintf(`
provider "metabase" {
  endpoint = "%s"
  username = "%s"
  password = "%s"
}
`,
	os.Getenv("METABASE_URL"),
	os.Getenv("METABASE_USERNAME"),
	os.Getenv("METABASE_PASSWORD"),
)

var providerApiKeyConfig = fmt.Sprintf(`
provider "metabase" {
	endpoint = "%s"
	api_key = "%s"
}
`,
	os.Getenv("METABASE_URL"),
	os.Getenv("METABASE_API_KEY"),
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"metabase": providerserver.NewProtocol6WithError(New("test")()),
}

var testAccMetabaseClient, _ = metabase.MakeAuthenticatedClientWithUsernameAndPassword(
	context.Background(),
	os.Getenv("METABASE_URL"),
	os.Getenv("METABASE_USERNAME"),
	os.Getenv("METABASE_PASSWORD"),
)
