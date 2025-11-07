APP_NAME = xplain
VERSION ?= dev
BUILD_DIR = bin
GORELEASER ?= go tool goreleaser
VERSION_VARIABLE = main.version

.PHONY: all build install uninstall clean test fmt lint release snapshot version

all: build

build:
	@echo "🔨 Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "-X $(VERSION_VARIABLE)=$(VERSION)" -o $(BUILD_DIR)/$(APP_NAME) .

install:
	@echo "📦 Installing $(APP_NAME)..."
	go install -ldflags "-X $(VERSION_VARIABLE)=$(VERSION)" .

uninstall:
	@echo "🗑️ Uninstalling $(APP_NAME)..."
	@bin_dir=$$(go env GOBIN); \
	if [ -z "$$bin_dir" ]; then \
		bin_dir=$$(go env GOPATH)/bin; \
	fi; \
	echo "Removing $$bin_dir/$(APP_NAME)"; \
	rm -f $$bin_dir/$(APP_NAME)

clean:
	@echo "🧹 Cleaning up..."
	rm -rf $(BUILD_DIR)

test:
	@echo "🧪 Running tests..."
	go test ./...

fmt:
	@echo "📝 Formatting code..."
	gofmt -w -l .

lint:
	go vet ./...
	go tool staticcheck ./...

version:
	@echo "⚙️  Version information"
	@echo "  App:      $(APP_NAME)"
	@echo "  Version:  $(VERSION)"
	@echo "  Variable: $(VERSION_VARIABLE)"

release:
	@echo "🚀 Running release..."
	$(GORELEASER) release --clean

snapshot:
	@echo "🔍 Running snapshot release (dry run)..."
	$(GORELEASER) release --snapshot --clean
