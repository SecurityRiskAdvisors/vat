# Project settings
 APP_NAME := vat
 VERSION := $(shell git describe --tags --always --dirty)
 BUILD_DIR := dist
 BUILD_LOCATION := ./cmd/
 GO_FILES := $(shell find . -type f -name '*.go' -not -path "./vendor/*")

 # Supported architectures for multi-arch builds
 ARCHS := amd64 arm64
 OS := linux darwin windows

 # Default target
 .PHONY: all
 all: clean get-tools deps generate schema-snapshot serializedschemavalidator build
 
 # Target to create a release for the build
 .PHONY: release
 release: clean deps generate test build-multiarch
	@echo "Release process completed successfully."
 
# release-branch tags an input of a tag, it creates a release branch and tags it
 .PHONY: release-branch
 release-branch: clean generate serializedschemavalidator vulncheck test release-checks
	@if [ -z "$(TAG)" ]; then \
		echo "Error: TAG parameter is required. Usage: make tag TAG=<tag_name>"; \
		git describe --tags --always --dirty; \
		exit 1; \
	fi
	@echo "Tagging release"
	@git tag -a $(TAG) -m "Tagging release $(TAG)"
	@echo "Creating a release branch with: $(TAG)..."
	@git branch "release/$(TAG)"
	@echo "Tagging complete."
	
# Pre-release checks to ensure the repository is in a valid state to create a release branch
.PHONY: release-checks
release-checks:
	@echo "Running release checks..."
# Check if the current branch is 'master'
	@if [ "$$(git rev-parse --abbrev-ref HEAD)" != "master" ]; then \
		echo "Error: You must be on the 'master' branch to create a release."; \
		exit 1; \
	fi
	@echo "Branch check passed: You are on 'master'."
# Check for unresolved merge conflicts
	@if git diff --name-only --diff-filter=U | grep -q .; then \
		echo "Error: There are unresolved merge conflicts. Please resolve them before releasing."; \
		exit 1; \
	fi
	@echo "Merge conflict check passed: No unresolved conflicts."
# Check if the working directory is clean
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory is dirty. Please commit or stash your changes before releasing."; \
		exit 1; \
	fi
	@echo "Working directory check passed: Working directory is clean."
	@echo "All release checks passed."

.PHONY: clean-deps
clean-deps:
	@go clean -testcache

 # Clean up the repository (remove build artifacts and temporary files)
 .PHONY: clean
 clean: clean-deps
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@rm -f internal/dao/generated.go
	@echo "Cleanup complete."
	
.PHONY: get-tools
get-tools: clean-deps
	@echo "Getting dev tools..."
	@cd tools && go mod download
	@echo "Dev tools fetched."
	
# Check for available dependency updates without applying them
.PHONY: check-updates
check-updates:
	@echo "Checking for dependency updates..."
	@go list -u -m -f '{{if .Update}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' all

.PHONY: update-prod-deps
update-prod-deps: clean-deps
	@echo "Updating production dependencies..."
	@go get -u -t ./...
	@go mod tidy
	@go mod download
	@echo "Production dependencies updated."

.PHONY: update-dev-deps
update-dev-deps: clean-deps get-tools
	@echo "Updating dev tool dependencies..."
	@cd tools && go get -u -tags tools ./... && go mod tidy
	@echo "Dev tool dependencies updated."

.PHONY: update-deps
update-deps: update-prod-deps update-dev-deps
	@echo "All dependencies updated."


# Pull dependencies
 .PHONY: deps
 deps:
	@echo "Pulling dependencies..."
	@go mod download
	@echo "Dependencies pulled."

.PHONY: generate
generate:
	@echo "Generating backend-code..."
	@mkdir -p internal/dao
	@go run -modfile=tools/go.mod github.com/Khan/genqlient
	@echo "Completed code generation."

 # Build the application for the current system
 .PHONY: build
 build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(APP_NAME) -ldflags "-X main.version=$(VERSION)" $(BUILD_LOCATION)
	@echo "Build complete. Binary is located in $(BUILD_DIR)/$(APP_NAME)."

# Multi-arch build
.PHONY: build-multiarch
build-multiarch:
	@echo "Building $(APP_NAME) for multiple architectures..."
	@mkdir -p $(BUILD_DIR)
	@for os in $(OS); do \
		for arch in $(ARCHS); do \
			output_name=$(BUILD_DIR)/$(APP_NAME)-$$os-$$arch; \
			if [ $$os = "windows" ]; then \
				output_name=$$output_name.exe; \
			fi; \
			echo "Building for $$os/$$arch..."; \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -o $$output_name -ldflags "-X main.version=$(VERSION)" $(BUILD_LOCATION) || exit 1; \
			if [ $$os = "windows" ]; then \
				zip -j $(BUILD_DIR)/$(APP_NAME)-$$os-$$arch-$(VERSION).zip $$output_name; \
			else \
				tar -czf $$output_name-$(VERSION).tar.gz -C $(BUILD_DIR) $(APP_NAME)-$$os-$$arch; \
			fi; \
			rm -f $$output_name; \
		done; \
	done
	@echo "Multi-arch build complete. Compressed binaries are located in $(BUILD_DIR)."
	 
 # Run tests
 .PHONY: test
 test:
	@echo "Running tests..."
	@go test ./... -v
	@echo "Tests complete."

.PHONY: vulncheck
vulncheck:
	@echo "Running vulncheck..."
	@go run -modfile=tools/go.mod golang.org/x/vuln/cmd/govulncheck ./...
	@echo "No vulns found."
	
.PHONY: serializedschemavalidator
serializedschemavalidator:
	@echo "Validating serialized output schema..."
	@go run _buildcode/objectvalidate/main.go > serializedschemahash.txt
	@echo "Completed, checked the file diff in git to validate no changes."

.PHONY: schema-diff
schema-diff:
	@echo "Diffing schema changes against types used in operations..."
	@mkdir -p _scratch
	@git show origin/master:graphql/schema/schema.gql > _scratch/old_schema.gql
	@TYPES_JSON=$$(go run ./_buildcode/schemavalidate/main.go --format json); \
	TYPES=$$(echo "$$TYPES_JSON" | jq -r '(.input + .output) | keys[]' | paste -sd'|'); \
	CHANGES=$$(docker run --rm -v $$PWD:/app kamilkisiela/graphql-inspector:v3.4.0 \
		graphql-inspector diff _scratch/old_schema.gql graphql/schema/schema.gql \
		| grep -P "\b($$TYPES)\b"); \
	if [ -z "$$CHANGES" ]; then \
		echo "No schema changes affect your operations."; \
	else \
		echo "$$TYPES_JSON" | jq -r '(.input + .output) | keys[]' | while read TYPE; do \
			TYPE_CHANGES=$$(echo "$$CHANGES" | grep -P "\b$$TYPE\b"); \
			[ -z "$$TYPE_CHANGES" ] && continue; \
			OPS=$$(echo "$$TYPES_JSON" | jq -r "(.input[\"$$TYPE\"] // .output[\"$$TYPE\"]).used_in | join(\", \")"); \
			echo "### $$TYPE (used in: $$OPS)"; \
			echo "$$TYPE_CHANGES"; \
			echo ""; \
		done; \
	fi

.PHONY: schema-snapshot
schema-snapshot:
	@echo "Generating schema type snapshot..."
	@go run -modfile=./_buildcode/schemavalidate/go.mod ./_buildcode/schemavalidate/main.go --snapshot > schematypes.txt
	@echo "Completed. Check git diff schematypes.txt to review changes."

.PHONY: create-draft-release
create-draft-release:
	@echo "Creating draft release on GitHub..."
	@gh release create $(VERSION) --draft --title "$(APP_NAME) $(VERSION)" --notes "Draft release for $(VERSION)"
	
.PHONY: add-files
add-files:
	@echo "Uploading files to draft release..."
	@gh release upload $(VERSION) $(BUILD_DIR)/* --clobber
	
.PHONY: push
push:
	@echo "Pushing in the correct sequence"
	@git push --tags origin master 'refs/heads/release/*'
