BINARY := eventsintel
PKG     := ./...
DB      ?= eventsintel.db

.PHONY: build test vet refresh-fixtures migrate

build:
	go build -o bin/$(BINARY) ./cmd/eventsintel

test:
	go test $(PKG)

vet:
	go vet $(PKG)

# refresh-fixtures re-captures source HTML/discovery fixtures. Implemented per
# adapter in Phase 2 (Task 0.3 convention: internal/sources/<venue>/testdata/).
refresh-fixtures:
	@echo "refresh-fixtures: not yet implemented (Phase 2)"

# migrate applies the embedded schema to $(DB) via the ingest path.
migrate: build
	./bin/$(BINARY) ingest

eval-report: ## Regenerate the Solar enrichment accuracy summary from audit results
	python3 eval/audit.py --report
