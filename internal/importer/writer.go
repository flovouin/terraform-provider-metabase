package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The default prefix for generated files, if none is specified.
const defaultFileNamePrefix = "mb-gen-"

// Options for the `ImportContext.Write` method.
type WriteOptions struct {
	FileNamePrefix              string // The prefix for generated files.
	DisableFileNameResourceType bool   // If `true`, each generated file name does not contain the type of resource defined in the file.
	ClearOutput                 bool   // If `true`, all files at the output path with the right prefix will be removed before generation.
}

// Returns either the prefix set in the options, or the default one.
func (wo *WriteOptions) getFileNamePrefix() string {
	if len(wo.FileNamePrefix) > 0 {
		return wo.FileNamePrefix
	}
	return defaultFileNamePrefix
}

// Removes all files in `path` with the prefix specified in the options (or the default one).
func clearOutput(path string, opts WriteOptions) error {
	glob := fmt.Sprintf("%s*.tf", filepath.Join(path, opts.getFileNamePrefix()))
	files, err := filepath.Glob(glob)
	if err != nil {
		return err
	}

	for _, f := range files {
		err := os.Remove(f)
		if err != nil {
			return err
		}
	}

	return nil
}

// Returns a file path for a given resource.
func makeFilePath(path string, resourceType string, slug string, opts WriteOptions) string {
	resourcePrefix := ""
	if !opts.DisableFileNameResourceType {
		resourcePrefix = fmt.Sprintf("%s-", resourceType)
	}

	slugWithDashes := strings.ReplaceAll(slug, "_", "-")
	fileName := fmt.Sprintf("%s%s%s.tf", opts.getFileNamePrefix(), resourcePrefix, slugWithDashes)

	return filepath.Join(path, fileName)
}

// Writes the tables, cards, and dashboards that have been imported to Terraform files.
func (ic *ImportContext) Write(path string, opts WriteOptions) error {
	if opts.ClearOutput {
		err := clearOutput(path, opts)
		if err != nil {
			return err
		}
	}

	for _, t := range ic.tables {
		path := makeFilePath(path, "table", t.Slug, opts)

		err := os.WriteFile(path, []byte(t.Hcl), 0644)
		if err != nil {
			return err
		}
	}

	for _, c := range ic.cards {
		path := makeFilePath(path, "card", c.Slug, opts)

		err := os.WriteFile(path, []byte(c.Hcl), 0644)
		if err != nil {
			return err
		}
	}

	for _, d := range ic.dashboards {
		path := makeFilePath(path, "dashboard", d.Slug, opts)

		err := os.WriteFile(path, []byte(d.Hcl), 0644)
		if err != nil {
			return err
		}
	}

	return nil
}
