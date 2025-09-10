package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccContentTranslationResource(name string, dictionary string) string {
	return fmt.Sprintf(`
resource "metabase_content_translation" "%s" {
  dictionary = <<-EOT
%s
EOT
}
`,
		name,
		dictionary,
	)
}

func testAccCheckContentTranslationExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		// For content translation, verify that we can retrieve the dictionary from the API
		response, err := testAccMetabaseClient.GetContentTranslationCsvWithResponse(context.Background())
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting content translation dictionary.")
		}

		// Verify that the dictionary content matches what we expect
		if rs.Primary.Attributes["dictionary"] != string(response.Body) {
			return fmt.Errorf("Terraform resource and API response do not match for content translation dictionary.")
		}

		return nil
	}
}

func testAccCheckContentTranslationDestroy(s *terraform.State) error {
	// For content translation, deletion means the dictionary is empty (just headers)
	response, err := testAccMetabaseClient.GetContentTranslationCsvWithResponse(context.Background())
	if err != nil {
		return err
	}
	if response.StatusCode() != 200 {
		return fmt.Errorf("Received unexpected response from the Metabase API when checking content translation dictionary.")
	}

	// Check if the dictionary is empty (just headers)
	expectedEmpty := "Language,String,Translation\n"
	if string(response.Body) != expectedEmpty {
		return fmt.Errorf("Content translation dictionary was not properly deleted. Expected empty dictionary, got: %s", string(response.Body))
	}

	return nil
}

func TestAccContentTranslationResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckContentTranslationDestroy,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccContentTranslationResource("test", `Language,String,Translation
en,Dashboard,Dashboard
fr,Dashboard,Tableau de bord
es,Dashboard,Tablero
en,Card,Card
fr,Card,Carte
es,Card,Tarjeta
en,Collection,Collection
fr,Collection,Collection
es,Collection,Colección
en,Database,Database
fr,Database,Base de données
es,Database,Base de datos
en,Table,Table
fr,Table,Table
es,Table,Tabla
en,Field,Field
fr,Field,Champ
es,Field,Campo
en,Question,Question
fr,Question,Question
es,Question,Pregunta`),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckContentTranslationExists("metabase_content_translation.test"),
					resource.TestCheckResourceAttr("metabase_content_translation.test", "id", "content-translation-dictionary"),
					resource.TestCheckResourceAttrSet("metabase_content_translation.test", "dictionary"),
					resource.TestCheckResourceAttrSet("metabase_content_translation.test", "content_hash"),
				),
			},
			// Update and Read testing
			{
				Config: providerConfig + testAccContentTranslationResource("test", `Language,String,Translation
en,Dashboard,Dashboard
fr,Dashboard,Tableau de bord
es,Dashboard,Tablero
de,Dashboard,Dashboard
it,Dashboard,Dashboard
en,Card,Card
fr,Card,Carte
es,Card,Tarjeta
de,Card,Karte
it,Card,Carta`),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckContentTranslationExists("metabase_content_translation.test"),
					resource.TestCheckResourceAttr("metabase_content_translation.test", "id", "content-translation-dictionary"),
					resource.TestCheckResourceAttrSet("metabase_content_translation.test", "dictionary"),
					resource.TestCheckResourceAttrSet("metabase_content_translation.test", "content_hash"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccContentTranslationResource_EmptyDictionary(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckContentTranslationDestroy,
		Steps: []resource.TestStep{
			// Create and Read testing with empty dictionary
			{
				Config: providerConfig + testAccContentTranslationResource("test_empty", `Language,String,Translation`),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckContentTranslationExists("metabase_content_translation.test_empty"),
					resource.TestCheckResourceAttr("metabase_content_translation.test_empty", "id", "content-translation-dictionary"),
					resource.TestCheckResourceAttr("metabase_content_translation.test_empty", "dictionary", "Language,String,Translation"),
					resource.TestCheckResourceAttrSet("metabase_content_translation.test_empty", "content_hash"),
				),
			},
		},
	})
}
