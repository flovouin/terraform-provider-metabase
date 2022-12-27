package importer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gosimple/slug"
)

// The regexp matching the placeholder for `metabase_table` data sources, accessing their `fields` attribute.
// The first group is the table and the second group is the name of the field (column).
var fieldRegexp = regexp.MustCompile("\\\"!!(data\\.metabase_table\\.\\w+\\.fields)\\[(\\w+)\\]!!\\\"")

// The regexp matching the placeholder for `metabase_table` data sources, accessing their `fields` attribute.
// The first group is the table and the second group is the name of the field (column).
// This regexp matches the placeholder when it has been serialized twice, and that the surrounding double quotes have
// been escaped.
var fieldInStringRegexp = regexp.MustCompile("\\\\\\\"!!(data\\.metabase_table\\.\\w+\\.fields)\\[(\\w+)\\]!!\\\\\\\"")

// The regexp matching the placeholder for `metabase_table` data sources.
// The captured group can be used as is in an HCL file.
var tableRegexp = regexp.MustCompile("\\\"!!(data\\.metabase_table\\.\\w+\\.id)!!\\\"")

// Marshals an `importedField` as a placeholder which references the corresponding Terraform table data source, and
// accesses the `field` attribute for this `metabase_table`.
func (f *importedField) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"!!data.metabase_table.%s.fields[%s]!!\"", f.ParentTable.Slug, f.Field.Name)), nil
}

// Marshals an `importedTable` as a placeholder which references the corresponding Terraform data source.
func (t *importedTable) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"!!data.metabase_table.%s.id!!\"", t.Slug)), nil
}

// Replaces all placeholders introduced by marshalling `imported*` structures to JSON.
// This produces a valid HCL snippet which references Metabase Terraform resources and data sources.
func replacePlaceholders(hcl string) string {
	hcl = fieldRegexp.ReplaceAllString(hcl, "$1[\"$2\"]")
	hcl = fieldInStringRegexp.ReplaceAllString(hcl, "${$1[\"$2\"]}")
	hcl = tableRegexp.ReplaceAllString(hcl, "$1")
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
