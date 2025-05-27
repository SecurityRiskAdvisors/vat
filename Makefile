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
 all: clean get-tools deps generate build
 
 # Target to create a release for the build
 .PHONY: release
 release: clean deps generate test build-multiarch
	@echo "Release process completed successfully."
 
# release-branch tags an input of a tag, it creates a release branch and tags it
# run update-deps before release checks to make sure that there are no module updates
# that are needed. If so, the release checks will block the release branch creation
 .PHONY: release-branch
 release-branch: clean update-deps generate vulncheck test release-checks
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
	@go clean -cache -testcache

 # Clean up the repository (remove build artifacts and temporary files)
 .PHONY: clean
 clean: clean-deps
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@rm -f generated.go
	@echo "Cleanup complete."
	
.PHONY: get-tools
get-tools: clean-deps
	@echo "Getting dev tools..."
	@go get -tool github.com/Khan/genqlient
	@go get -tool golang.org/x/vuln/cmd/govulncheck@latest
	@go mod tidy
	@echo "Dev tools fetched."
	
# Use this to update deps and get dev requirements
.PHONY: update-deps
update-deps: clean-deps get-tools
	@echo "Pulling dependencies..."
	@go get -u -t .
	@go mod tidy
	@go mod download
	@echo "Dependencies pulled."


# Pull dependencies
 .PHONY: deps
 deps:
	@echo "Pulling dependencies..."
	@go mod download
	@echo "Dependencies pulled."

.PHONY: generate
generate:
	@echo "Generating backend-code..."
	@go run github.com/Khan/genqlient
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
	@echo "Running tests..."
	@go run golang.org/x/vuln/cmd/govulncheck ./...
	@echo "No vulns found."

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