# Application settings
resource "metabase_setting" "app_name" {
  key   = "application-name"
  value = "Mon Instance Metabase"
}

resource "metabase_setting" "app_favicon" {
  key   = "application-favicon-url"
  value = "https://example.com/favicon.ico"
}

# Email settings
resource "metabase_setting" "email_from_name" {
  key   = "email-from-name"
  value = "Metabase"
}

resource "metabase_setting" "email_from_address" {
  key   = "email-from-address"
  value = "noreply@example.com"
}

# Security settings
resource "metabase_setting" "session_timeout" {
  key   = "session-timeout"
  value = "1440" # 24 hours in minutes
}

resource "metabase_setting" "password_complexity" {
  key   = "password-complexity"
  value = "strong"
}

# UI settings
resource "metabase_setting" "site_locale" {
  key   = "site-locale"
  value = "en"
}

resource "metabase_setting" "custom_geojson" {
  key   = "custom-geojson"
  value = "{}"
}

# Database settings
resource "metabase_setting" "database_automatic_queries" {
  key   = "database-automatic-queries"
  value = "true"
}

resource "metabase_setting" "database_connection_timeout" {
  key   = "database-connection-timeout"
  value = "30000"
}
