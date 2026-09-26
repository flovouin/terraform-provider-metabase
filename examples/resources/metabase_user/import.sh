# Use the integer ID from the Metabase API. Deactivated users can be imported as well, and are imported with
# `is_active` set to `false`. They are never reactivated by the import.
terraform import metabase_user.jane 3
