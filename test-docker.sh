# This scripts sets up (or tears down, by passing `tear-down` as the first argument) a Docker environment for running
# the acceptance tests of the Terraform provider.
# Additionally to the Metabase instance, it also sets up a Postgres database, which is used as a dummy database for the
# Metabase database management tests.

DOCKER_NETWORK=terraform-metabase

METABASE_IMAGE=${METABASE_IMAGE:=metabase/metabase}
METABASE_CONTAINER_NAME=terraform-metabase-mb
METABASE_VERSION=${METABASE_VERSION:=v0.56.5}
METABASE_PORT=${METABASE_PORT:=3000}
METABASE_USERNAME=${METABASE_USERNAME:=terraform-provider@tests.com}
METABASE_PASSWORD=${METABASE_PASSWORD:=$(uuidgen)}
METABASE_URL=http://localhost:${METABASE_PORT}

PG_CONTAINER_NAME=terraform-metabase-pg
PG_VERSION=16
PG_USER=${PG_USER:=metabase}
PG_PASSWORD=${PG_PASSWORD:=$(uuidgen)}
PG_DATABASE=${PG_DATABASE:=metabase}

SETUP_TOKEN_MAX_ATTEMPTS=10

TEST_API_KEY_FILE=.test-api-key

# Sets up a non-default Docker network, such that the containers can communicate using hostnames.
set_up_network () {
  echo "🌐 Creating Docker network..." 1>&2
  docker network inspect ${DOCKER_NETWORK} > /dev/null 2>&1 || \
  docker network create ${DOCKER_NETWORK} > /dev/null
}

tear_down_network () {
  if [ -n "$(docker network ls -q -f name=${DOCKER_NETWORK})" ]; then
    echo "🔥 Removing Docker network..." 1>&2
    docker network rm ${DOCKER_NETWORK} > /dev/null
  fi
}

# Postgres database to test Metabase database management.
tear_down_pg () {
  if [ -n "$(docker ps -aq -f name=${PG_CONTAINER_NAME})" ]; then
    echo "🔥 Removing Postgres container..." 1>&2
    docker rm -f ${PG_CONTAINER_NAME} > /dev/null
  fi
}

set_up_pg () {
  tear_down_pg

  echo "🐳 Starting Postgres container..." 1>&2
  docker run -d \
    --name ${PG_CONTAINER_NAME} \
    --network ${DOCKER_NETWORK} \
    -e POSTGRES_USER=${PG_USER} \
    -e POSTGRES_PASSWORD=${PG_PASSWORD} \
    -e POSTGRES_DB=${PG_DATABASE} \
    postgres:${PG_VERSION} > /dev/null
}

# Metabase itself.
tear_down_metabase () {
  if [ -n "$(docker ps -aq -f name=${METABASE_CONTAINER_NAME})" ]; then
    echo "🔥 Removing Metabase container..." 1>&2
    docker rm -f ${METABASE_CONTAINER_NAME} > /dev/null
  fi
}

set_up_metabase () {
  tear_down_metabase

  echo "🐳 Starting Metabase container..." 1>&2
  docker run -d \
    --name ${METABASE_CONTAINER_NAME} \
    --network ${DOCKER_NETWORK} \
    -p ${METABASE_PORT}:${METABASE_PORT} \
    ${METABASE_IMAGE}:${METABASE_VERSION} > /dev/null
}

# When Metabase starts up, it must be configured by setting up a first user.
# This is usually done using the web UI, providing a setup token.
# The following functions mock this process by fetching the setup token from the landing page and using it to call the
# Metabase API to set up the user.
fetch_setup_token () {
  SETUP_TOKEN=""
  NUM_ATTEMPTS=0

  while [ -z "${SETUP_TOKEN}" ]; do
    sleep 5

    NUM_ATTEMPTS=$((NUM_ATTEMPTS + 1))
    if [ ${NUM_ATTEMPTS} -gt ${SETUP_TOKEN_MAX_ATTEMPTS} ]; then
      echo "❌ Failed to fetch setup token after ${SETUP_TOKEN_MAX_ATTEMPTS} attempts." 1>&2
      exit 1
    else
      echo "👽 Fetching setup token (attempt ${NUM_ATTEMPTS})..." 1>&2
    fi

    LANDING_PAGE=$(
      curl ${METABASE_URL} \
        --retry-max-time 120 \
        --retry 20 \
        --retry-all-errors \
        --silent \
        2>&1
    )

    SETUP_TOKEN=$(
      echo "${LANDING_PAGE}" | \
      sed -nE 's/.*"setup-token":\s*"([^"]+)".*/\1/p'
    )
  done

  echo ${SETUP_TOKEN}
}

set_metabase_credentials () {
  SETUP_TOKEN=$(fetch_setup_token)

  echo "🔧 Setting Metabase credentials..." 1>&2
  API_RESPONSE=$(
    curl -X POST ${METABASE_URL}/api/setup \
      --silent \
      --fail-with-body \
      --header "Content-Type: application/json" \
      --data-binary @- << EOF
{
  "token": "${SETUP_TOKEN}",
  "user": {
    "password_confirm": "${METABASE_PASSWORD}",
    "password": "${METABASE_PASSWORD}",
    "email": "${METABASE_USERNAME}"
  },
  "prefs": { "site_name": "Terraform provider" }
}
EOF
  )

  if [ $? -ne 0 ]; then
    echo "❌ Failed to set Metabase credentials:
${API_RESPONSE}" 1>&2
    exit 1
  fi
}

set_api_key () {
  echo "🔑 Creating API key..." 1>&2

  METABASE_SESSION_RESPONSE=$(
    curl -X POST ${METABASE_URL}/api/session \
      --silent \
      --fail-with-body \
      --header 'Content-Type: application/json' \
      --data-binary @- << EOF
{
  "username": "${METABASE_USERNAME}",
  "password": "${METABASE_PASSWORD}"
}
EOF
  )

  if [ $? -ne 0 ]; then
    echo "❌ Failed to create Metabase session:
${API_RESPONSE}" 1>&2
    exit 1
  fi

  METABASE_SESSION=$(
    echo "${METABASE_SESSION_RESPONSE}" | \
    jq -r '.id'
  )

  METABASE_API_KEY_RESPONSE=$(
    curl -X POST ${METABASE_URL}/api/api-key/ \
      --silent \
      --fail-with-body \
      --header "X-Metabase-Session: ${METABASE_SESSION}" \
      --header "Content-Type: application/json" \
      --data-binary @- << EOF
{
  "group_id": 2,
  "name": "Terraform tests"
}
EOF
  )

  if [ $? -ne 0 ]; then
    echo "❌ Failed to create API key:
${METABASE_API_KEY_RESPONSE}" 1>&2
    exit 1
  fi

  METABASE_API_KEY=$(
    echo "${METABASE_API_KEY_RESPONSE}" | \
    jq -r '.unmasked_key'
  )

  echo "${METABASE_API_KEY}" > ${TEST_API_KEY_FILE}
  echo "${METABASE_API_KEY}"
}

# Entry point.
set_up () {
  set_up_network
  set_up_pg
  set_up_metabase
  set_metabase_credentials
  METABASE_API_KEY=$(set_api_key)

  echo "✅ Setup complete.

🌐 Docker network: ${DOCKER_NETWORK}
🐘 Postgres:
  🔗 Hostname (within Docker network): ${PG_CONTAINER_NAME}
  👤 User: ${PG_USER}
  🔒 Password: ${PG_PASSWORD}
  🗃️ Database: ${PG_DATABASE}
📈 Metabase:
  🔗 URL: ${METABASE_URL}
  👤 Email: ${METABASE_USERNAME}
  🔒 Password: ${METABASE_PASSWORD}
  🔑 API key: ${METABASE_API_KEY}" 1>&2
}

tear_down () {
  tear_down_metabase
  tear_down_pg
  tear_down_network

  echo "✅ Tear down complete." 1>&2
}

if [ "$1" = "tear-down" ]; then
  tear_down
  exit 0
fi

set_up
