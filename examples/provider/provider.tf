provider "metabase" {
  endpoint = "http://metabase-endpoint.com/api"

  # Authentication can be done using a username and password...
  username = "email@address.com"
  password = "password"

  # ...or using an API key.
  # api_key = "API key"

  # When Metabase sits behind a proxy expecting credentials of its own, those can be passed as extra headers, sent with
  # every request. For example, a Cloudflare Access service token:
  # extra_headers = {
  #   "CF-Access-Client-Id"     = "<client id>.access"
  #   "CF-Access-Client-Secret" = "<client secret>"
  # }
}
