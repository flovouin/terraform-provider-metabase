.EXPORT_ALL_VARIABLES:
.PHONY: set-up-docker tear-down-docker testacc testacc-with-setup clean-testacc provider clean generate

METABASE_USERNAME?=terraform-provider@tests.com
METABASE_PASSWORD?=$(shell uuidgen)
METABASE_URL?=http://localhost:3000/api

PG_HOST?=terraform-metabase-pg
PG_USER?=metabase
PG_PASSWORD?=$(shell uuidgen)
PG_DATABASE?=metabase

MBTF_FOLDER:=cmd/mbtf

METABASE_CLIENT_FILES:=$(shell find metabase -type f -name '*.go')
PROVIDER_FILES:=$(shell find internal/provider -type f -name '*.go')
PLAN_MODIFIER_FILES:=$(shell find internal/planmodifiers -type f -name '*.go')
MBTF_FILES:=$(shell find $(MBTF_FOLDER) -type f -name '*.go')

PROVIDER_BINARY:=terraform-provider-metabase
MBTF_BINARY:=mbtf

set-up-docker:
	./test-docker.sh

tear-down-docker:
	./test-docker.sh tear-down

testacc:
	TF_ACC=1 go test ./... -v

testacc-with-setup: set-up-docker testacc tear-down-docker

clean-testacc:
	go clean -testcache

metabase/client.gen.go: main.go
	go generate

generate:
	go generate

$(PROVIDER_BINARY): $(PROVIDER_FILES) $(METABASE_CLIENT_FILES) $(PLAN_MODIFIER_FILES)
	go build

provider: $(PROVIDER_BINARY)

$(MBTF_BINARY): $(METABASE_CLIENT_FILES) $(MBTF_FILES)
	cd $(MBTF_FOLDER) && go build -o $(MBTF_BINARY)
	mv $(MBTF_FOLDER)/$(MBTF_BINARY) .

clean: tear-down-docker clean-testacc
	rm -f $(PROVIDER_BINARY)
	rm -f $(MBTF_BINARY)
