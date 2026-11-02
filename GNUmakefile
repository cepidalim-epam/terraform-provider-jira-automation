HOSTNAME=registry.terraform.io
NAMESPACE=beno
NAME=jira-automation
BINARY=terraform-provider-${NAME}
VERSION=0.1.0
OS_ARCH=$(shell go env GOOS)_$(shell go env GOARCH)

default: install

build:
	go build -o ${BINARY}

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	cp ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}/

test:
	go test ./... -short -v

testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

generate:
	go generate ./...

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

golden:
	TF_ACC=1 GOLDEN_UPDATE=1 go test ./internal/provider/ -v -run TestAccDocExample -timeout 30m

lint-env:
	dotenv-linter check --ignore-checks UnorderedKey .env.example
	@if [ -f .env ]; then dotenv-linter check --schema .env.schema --ignore-checks UnorderedKey .env && dotenv-linter diff .env.example .env; fi

dev.tfrc: build
	@printf 'provider_installation {\n  dev_overrides {\n    "registry.terraform.io/cepicdalim-epam/terraform-provider-jira-automation" = "%s"\n  }\n  direct {}\n}\n' "$(CURDIR)" > dev.tfrc

.PHONY: build install test testacc generate docs golden lint-env
