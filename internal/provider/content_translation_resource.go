package provider

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime/multipart"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ContentTranslationResource{}

// Creates a new content translation resource.
func NewContentTranslationResource() resource.Resource {
	return &ContentTranslationResource{
		MetabaseBaseResource{name: "content_translation"},
	}
}

// A resource handling Metabase content translations (Enterprise Edition feature).
type ContentTranslationResource struct {
	MetabaseBaseResource
}

// The Terraform model for content translations.
type ContentTranslationResourceModel struct {
	Id          types.String `tfsdk:"id"`           // A unique identifier for the translation set.
	Dictionary  types.String `tfsdk:"dictionary"`   // The CSV content of the translation dictionary.
	ContentHash types.String `tfsdk:"content_hash"` // SHA256 hash of the dictionary content for state management.
}

// calculateContentHash computes a SHA256 hash of the dictionary content.
func calculateContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash)
}

// Updates the given `ContentTranslationResourceModel` from the dictionary content.
func updateModelFromContentTranslation(dictionary string, data *ContentTranslationResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.StringValue("content-translation-dictionary")
	data.Dictionary = types.StringValue(dictionary)
	data.ContentHash = types.StringValue(calculateContentHash(dictionary))

	return diags
}

// uploadContentTranslationDictionary uploads the given dictionary content to Metabase.
func (r *ContentTranslationResource) uploadContentTranslationDictionary(ctx context.Context, dictionary string) diag.Diagnostics {
	// Create multipart form data for file upload
	body := &strings.Builder{}
	writer := multipart.NewWriter(body)

	// Create form file field
	fileWriter, err := writer.CreateFormFile("file", "translations.csv")
	if err != nil {
		return diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Error creating form file",
				fmt.Sprintf("Could not create form file: %s", err),
			),
		}
	}

	// Write the CSV content to the form file
	_, err = io.WriteString(fileWriter, dictionary)
	if err != nil {
		return diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Error writing CSV content",
				fmt.Sprintf("Could not write CSV content: %s", err),
			),
		}
	}

	// Close the multipart writer
	err = writer.Close()
	if err != nil {
		return diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Error closing multipart writer",
				fmt.Sprintf("Could not close multipart writer: %s", err),
			),
		}
	}

	// Upload the translation dictionary
	uploadResp, err := r.client.UploadContentTranslationDictionaryWithBodyWithResponse(
		ctx,
		"multipart/form-data; boundary="+writer.Boundary(),
		strings.NewReader(body.String()),
	)

	return checkMetabaseResponse(uploadResp, err, []int{200}, "upload content translation dictionary")
}

func (r *ContentTranslationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Metabase content translation dictionary. This resource manages the translation dictionary for Metabase Enterprise Edition. The dictionary content is stored as a hash in the state for efficiency.\n\n## ⚠️ Important Warning\n\n**Deleting this resource will erase all translation dictionaries on the Metabase instance.** When you destroy this resource, it uploads an empty dictionary to Metabase, effectively removing all translations. Make sure to backup your translation data before destroying this resource if you need to preserve the translations.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "A unique identifier for the translation set.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dictionary": schema.StringAttribute{
				MarkdownDescription: "The CSV content of the translation dictionary. Must have columns: Locale Code (locale code), String (text to translate), Translation (translated text). Example: `Locale Code,String,Translation\\npt-BR,Examples,Exemplos\\nen,Dashboard,Dashboard`",
				Required:            true,
			},
			"content_hash": schema.StringAttribute{
				MarkdownDescription: "SHA256 hash of the dictionary content, used for change detection and state management.",
				Computed:            true,
			},
		},
	}
}

func (r *ContentTranslationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *ContentTranslationResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Upload the translation dictionary
	resp.Diagnostics.Append(r.uploadContentTranslationDictionary(ctx, data.Dictionary.ValueString())...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update the model with computed values
	resp.Diagnostics.Append(updateModelFromContentTranslation(data.Dictionary.ValueString(), data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ContentTranslationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *ContentTranslationResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If we have a dictionary in state, verify it's still current by checking the hash
	// If no dictionary in state (e.g., after import), fetch it from the API
	if data.Dictionary.IsNull() || data.Dictionary.IsUnknown() {
		// Fetch current dictionary from Metabase API
		csvResp, err := r.client.GetContentTranslationCsvWithResponse(ctx)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Could not fetch current dictionary",
				fmt.Sprintf("Failed to fetch current dictionary from Metabase: %s. Using state data.", err),
			)
		} else if csvResp.StatusCode() == 200 {
			resp.Diagnostics.Append(updateModelFromContentTranslation(string(csvResp.Body), data)...)
		}
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ContentTranslationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *ContentTranslationResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Upload the updated translation dictionary
	resp.Diagnostics.Append(r.uploadContentTranslationDictionary(ctx, data.Dictionary.ValueString())...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update the model with computed values
	resp.Diagnostics.Append(updateModelFromContentTranslation(data.Dictionary.ValueString(), data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ContentTranslationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// For content translation, deletion means uploading an empty dictionary
	// This effectively removes all translations
	emptyDictionary := "Locale Code,String,Translation\n"

	resp.Diagnostics.Append(r.uploadContentTranslationDictionary(ctx, emptyDictionary)...)
}
