package importer

import (
	"fmt"
	"strings"

	"github.com/gosimple/slug"
)

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
