package importer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gosimple/slug"
)

// The regexp matching the placeholder for `metabase_card` resources.
// The captured group can be used as is in an HCL file.
var cardRegexp = regexp.MustCompile("\\\"!!(metabase_card\\.\\w+\\.id)!!\\\"")

// The regexp matching the placeholder for `metabase_table` data sources, accessing their `fields` attribute.
// The first group is the table and the second group is the name of the field (column).
var fieldRegexp = regexp.MustCompile("\\\"!!(metabase_table\\.\\w+\\.fields)\\[(\\w+)\\]!!\\\"")

// The regexp matching the placeholder for `metabase_table` data sources, accessing their `fields` attribute.
// The first group is the table and the second group is the name of the field (column).
// This regexp matches the placeholder when it has been serialized twice, and that the surrounding double quotes have
// been escaped.
var fieldInStringRegexp = regexp.MustCompile("\\\\\\\"!!(metabase_table\\.\\w+\\.fields)\\[(\\w+)\\]!!\\\\\\\"")

// The regexp matching the placeholder for `metabase_table` data sources.
// The captured group can be used as is in an HCL file.
var tableRegexp = regexp.MustCompile("\\\"!!(metabase_table\\.\\w+\\.id)!!\\\"")

// The regexp matching the placeholder for `metabase_database` resources.
// The captured group can be used as is in an HCL file.
var databaseRegexp = regexp.MustCompile("\\\"!!(metabase_database\\.\\w+\\.id)!!\\\"")

// The regexp matching the placeholder for `metabase_collection` resources.
// The captured group can be used as is in an HCL file.
var collectionRegexp = regexp.MustCompile("\\\"!!(tonumber\\(metabase_collection\\.\\w+\\.id\\))!!\\\"")

// Marshals an `importedCard` as a placeholder which references the corresponding Terraform resource.
func (c *importedCard) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "\"!!metabase_card.%s.id!!\"", c.Slug), nil
}

// Marshals an `importedField` as a placeholder which references the corresponding Terraform table data source, and
// accesses the `field` attribute for this `metabase_table`.
func (f *importedField) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "\"!!metabase_table.%s.fields[%s]!!\"", f.ParentTable.Slug, f.Field.Name), nil
}

// Marshals an `importedTable` as a placeholder which references the corresponding Terraform data source.
func (t *importedTable) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "\"!!metabase_table.%s.id!!\"", t.Slug), nil
}

// Marshals an `importedDatabase` as a placeholder which references the corresponding Terraform resource.
func (d *importedDatabase) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "\"!!metabase_database.%s.id!!\"", d.Slug), nil
}

// Marshals an `importedCollection` as a placeholder which references the corresponding Terraform resource.
// Only collections with an integer ID are supported.
func (c *importedCollection) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "\"!!tonumber(metabase_collection.%s.id)!!\"", c.Slug), nil
}

// Replaces all placeholders introduced by marshalling `imported*` structures to JSON.
// This produces a valid HCL snippet which references Metabase Terraform resources and data sources.
func replacePlaceholders(hcl string) string {
	hcl = cardRegexp.ReplaceAllString(hcl, "$1")
	hcl = fieldRegexp.ReplaceAllString(hcl, "$1[\"$2\"]")
	hcl = fieldInStringRegexp.ReplaceAllString(hcl, "${$1[\"$2\"]}")
	hcl = tableRegexp.ReplaceAllString(hcl, "$1")
	hcl = databaseRegexp.ReplaceAllString(hcl, "$1")
	hcl = collectionRegexp.ReplaceAllString(hcl, "$1")
	return hcl
}

// Makes a unique slug containing underscores instead of dashes.
// The returned slug is guaranteed not to existing in `existingSlugs`. When this function returns, the slug has been
// added to the map passed as input.
func makeUniqueSlug(str string, existingSlugs map[string]bool) string {
	slug.MaxLength = 124 // Leaving 4 characters for the suffix in case of duplicates.

	slg := slug.Make(str)
	slg = strings.ReplaceAll(slg, "-", "_")
	baseSlug := slg

	for i := 1; ; i++ {
		_, exists := existingSlugs[slg]
		if !exists {
			existingSlugs[slg] = true
			return slg
		}

		slg = fmt.Sprintf("%s_%03d", baseSlug, i)
	}
}
