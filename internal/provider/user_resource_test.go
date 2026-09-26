package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Metabase never deletes users, and the email address of a deactivated user stays in use. Tests therefore generate a
// unique email address for every run, so that they can be run repeatedly against the same Metabase instance.
func testAccUserEmail(prefix string) string {
	return fmt.Sprintf("%s-%s@tests.com", prefix, acctest.RandString(8))
}

func testAccUserResource(name string, email string, firstName string, lastName string, extraAttributes string) string {
	return fmt.Sprintf(`
resource "metabase_user" "%s" {
  email      = "%s"
  first_name = "%s"
  last_name  = "%s"
%s
}
`,
		name,
		email,
		firstName,
		lastName,
		extraAttributes,
	)
}

// Returns the user with the given ID from the Metabase API, including deactivated users.
func testAccGetUser(userId int64) (*metabase.User, error) {
	response, err := testAccMetabaseClient.GetUserWithResponse(context.Background(), int(userId))
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("Received unexpected response from the Metabase API when getting user: %d.", response.StatusCode())
	}

	return response.JSON200, nil
}

// Returns the ID of the user for the given resource in the Terraform state.
func testAccUserIdFromState(s *terraform.State, resourceName string) (int64, error) {
	rs, ok := s.RootModule().Resources[resourceName]
	if !ok {
		return 0, fmt.Errorf("Failed to find resource %s in state.", resourceName)
	}

	return strconv.ParseInt(rs.Primary.ID, 10, 64)
}

func testAccCheckUserExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		userId, err := testAccUserIdFromState(s, resourceName)
		if err != nil {
			return err
		}

		user, err := testAccGetUser(userId)
		if err != nil {
			return err
		}

		rs := s.RootModule().Resources[resourceName]
		if rs.Primary.Attributes["email"] != user.Email {
			return fmt.Errorf("Terraform resource and API response do not match for user email.")
		}

		return nil
	}
}

// Checks that the user is reported as active (or not) by the Metabase API, independently of the Terraform state.
func testAccCheckUserIsActive(resourceName string, expectedActive bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		userId, err := testAccUserIdFromState(s, resourceName)
		if err != nil {
			return err
		}

		user, err := testAccGetUser(userId)
		if err != nil {
			return err
		}

		isActive := user.IsActive == nil || *user.IsActive
		if isActive != expectedActive {
			return fmt.Errorf("Expected the Metabase API to report is_active = %t for user %d, got %t.", expectedActive, userId, isActive)
		}

		return nil
	}
}

// Deactivates the user for the given resource using the Metabase API directly, simulating a change made outside of
// Terraform (e.g. an administrator deactivating an employee that left the company). This is exposed as a check because
// `PreConfig` does not have access to the Terraform state, which is where the ID of the user is found.
func testAccDeactivateUserOutOfBand(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		userId, err := testAccUserIdFromState(s, resourceName)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.DeactivateUserWithResponse(context.Background(), int(userId))
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when deactivating user: %d.", response.StatusCode())
		}

		return nil
	}
}

// Deleting a user resource deactivates it: Metabase does not support permanently deleting users.
func testAccCheckUserDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_user" {
			continue
		}

		userId, err := strconv.ParseInt(rs.Primary.ID, 10, 64)
		if err != nil {
			return err
		}

		user, err := testAccGetUser(userId)
		if err != nil {
			return err
		}

		if user.IsActive != nil && *user.IsActive {
			return fmt.Errorf("User %s is still active.", rs.Primary.ID)
		}
	}

	return nil
}

func TestAccUserResource(t *testing.T) {
	email := testAccUserEmail("terraform-user")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckUserDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig +
					testAccPermissionsGroupResource("group", "🧑‍🚀 User group") +
					testAccUserResource("test", email, "Terra", "Form", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckUserExists("metabase_user.test"),
					resource.TestCheckResourceAttrSet("metabase_user.test", "id"),
					resource.TestCheckResourceAttr("metabase_user.test", "email", email),
					resource.TestCheckResourceAttr("metabase_user.test", "first_name", "Terra"),
					resource.TestCheckResourceAttr("metabase_user.test", "last_name", "Form"),
					resource.TestCheckResourceAttr("metabase_user.test", "common_name", "Terra Form"),
					resource.TestCheckResourceAttr("metabase_user.test", "is_superuser", "false"),
					resource.TestCheckResourceAttr("metabase_user.test", "is_active", "true"),
					// The `All Users` group is implicit and is never reported.
					resource.TestCheckResourceAttr("metabase_user.test", "group_ids.#", "0"),
					resource.TestCheckResourceAttrSet("metabase_user.test", "date_joined"),
				),
			},
			{
				ResourceName: "metabase_user.test",
				ImportState:  true,
			},
			{
				// Updating the user, and adding it to a group.
				Config: providerConfig +
					testAccPermissionsGroupResource("group", "🧑‍🚀 User group") +
					testAccUserResource("test", email, "Terra", "Formed", `
  locale    = "fr"
  group_ids = [metabase_permissions_group.group.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckUserExists("metabase_user.test"),
					resource.TestCheckResourceAttr("metabase_user.test", "last_name", "Formed"),
					resource.TestCheckResourceAttr("metabase_user.test", "locale", "fr"),
					resource.TestCheckResourceAttr("metabase_user.test", "group_ids.#", "1"),
					resource.TestCheckResourceAttrPair("metabase_user.test", "group_ids.0", "metabase_permissions_group.group", "id"),
					resource.TestCheckResourceAttr("metabase_user.test", "is_active", "true"),
					testAccCheckUserIsActive("metabase_user.test", true),
				),
			},
			{
				// Making the user an administrator. The `Administrators` group is managed through `is_superuser` and is
				// not reported in `group_ids`.
				Config: providerConfig +
					testAccPermissionsGroupResource("group", "🧑‍🚀 User group") +
					testAccUserResource("test", email, "Terra", "Formed", `
  locale       = "fr"
  is_superuser = true
  group_ids    = [metabase_permissions_group.group.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_user.test", "is_superuser", "true"),
					resource.TestCheckResourceAttr("metabase_user.test", "group_ids.#", "1"),
					resource.TestCheckResourceAttr("metabase_user.test", "is_active", "true"),
					testAccCheckUserIsActive("metabase_user.test", true),
				),
			},
			{
				// Explicitly deactivating the user.
				Config: providerConfig +
					testAccPermissionsGroupResource("group", "🧑‍🚀 User group") +
					testAccUserResource("test", email, "Terra", "Formed", `
  locale       = "fr"
  is_superuser = true
  is_active    = false
  group_ids    = [metabase_permissions_group.group.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_user.test", "is_active", "false"),
					testAccCheckUserIsActive("metabase_user.test", false),
				),
			},
			{
				// Explicitly reactivating the user.
				Config: providerConfig +
					testAccPermissionsGroupResource("group", "🧑‍🚀 User group") +
					testAccUserResource("test", email, "Terra", "Formed", `
  locale       = "fr"
  is_superuser = true
  is_active    = true
  group_ids    = [metabase_permissions_group.group.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_user.test", "is_active", "true"),
					resource.TestCheckResourceAttr("metabase_user.test", "is_superuser", "true"),
					resource.TestCheckResourceAttr("metabase_user.test", "group_ids.#", "1"),
					testAccCheckUserIsActive("metabase_user.test", true),
				),
			},
		},
	})
}

// Checks that a user deactivated outside of Terraform is *not* silently reactivated by a refresh. This is the main
// reason why this resource does not treat a missing user as an invitation to reactivate it.
func TestAccUserResourceDoesNotReactivateOnRefresh(t *testing.T) {
	config := providerConfig + testAccUserResource("leaver", testAccUserEmail("terraform-leaver"), "Gone", "Away", "")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckUserDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckUserExists("metabase_user.leaver"),
					resource.TestCheckResourceAttr("metabase_user.leaver", "is_active", "true"),
					// The user is deactivated outside of Terraform, right before the state is refreshed by the next step.
					testAccDeactivateUserOutOfBand("metabase_user.leaver"),
				),
			},
			{
				// The refresh must report the deactivation instead of undoing it, and must leave the user deactivated in
				// Metabase. A provider reactivating users when they cannot be read would silently restore access here.
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_user.leaver", "is_active", "false"),
					testAccCheckUserIsActive("metabase_user.leaver", false),
				),
			},
		},
	})
}

// Checks that updating an attribute of a user does not have side effects on the attributes that are left out of the
// configuration. `is_active` and `group_ids` are both optional and computed, and are therefore unknown in the plan when
// the configuration does not set them: reading those unknown values as "inactive" and "no group at all" would deactivate
// the user and strip its permissions on every unrelated change.
func TestAccUserResourceUpdateKeepsOmittedAttributes(t *testing.T) {
	email := testAccUserEmail("terraform-omitted")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckUserDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig +
					testAccPermissionsGroupResource("group", "🧑‍🏫 Omitted attributes group") +
					testAccUserResource("test", email, "Kept", "Around", `
  group_ids = [metabase_permissions_group.group.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_user.test", "group_ids.#", "1"),
					resource.TestCheckResourceAttr("metabase_user.test", "is_active", "true"),
				),
			},
			{
				// Only the first name changes. Neither `is_active` nor `group_ids` appear in the configuration anymore,
				// and both must keep the value they already have, in Terraform and in Metabase.
				Config: providerConfig +
					testAccPermissionsGroupResource("group", "🧑‍🏫 Omitted attributes group") +
					testAccUserResource("test", email, "Still", "Around", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_user.test", "first_name", "Still"),
					resource.TestCheckResourceAttr("metabase_user.test", "is_active", "true"),
					testAccCheckUserIsActive("metabase_user.test", true),
					resource.TestCheckResourceAttr("metabase_user.test", "group_ids.#", "1"),
					resource.TestCheckResourceAttrPair("metabase_user.test", "group_ids.0", "metabase_permissions_group.group", "id"),
				),
			},
		},
	})
}
