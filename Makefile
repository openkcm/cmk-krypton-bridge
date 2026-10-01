.PHONY: all build test test-integration lint proto docker helm-package run tidy buf-init \
        demo demo-mock \
        demo-health demo-ensure-l2 demo-ensure-l3 demo-ensure-l4 \
        demo-idempotency demo-list-keys demo-new-tenant \
        db-up db-down db-migrate db-reset

BINARY_PROXY  = bin/cmk-krypton-bridge
BINARY_CLI    = bin/cmk2kryptonctl
PROTO_DIR     = proto
GEN_DIR       = internal/gen
IMAGE_TAG     = openkcm/cmk-krypton-bridge:latest
PROXY_ADDR    = localhost:9092

# Tenant + service for demo (override on CLI: make demo TENANT=myco SERVICE=billing)
TENANT        ?= demo-poc
SERVICE       ?= payments

# CLI shared flags
CLI = $(BINARY_CLI) --addr=$(PROXY_ADDR)

# grpcurl shared flags (uses proto file, no reflection needed)
GRPCURL = grpcurl -plaintext \
  -proto proto/kms/api/krypton/rpc/v1/proxy_service.proto \
  -import-path . \
  -authority localhost

# ── build ─────────────────────────────────────────────────────────────────────

all: proto build test

proto:
	@which buf > /dev/null || (echo "buf not installed: https://buf.build/docs/installation" && exit 1)
	buf generate

build:
	go build -o $(BINARY_PROXY) ./cmd/proxy
	go build -o $(BINARY_CLI)   ./cmd/cli

tidy:
	go mod tidy

# ── tests ─────────────────────────────────────────────────────────────────────

test:
	go test -race ./...

test-integration:
	go test -tags integration -race ./...

lint:
	golangci-lint run ./...

# ── run ───────────────────────────────────────────────────────────────────────

run:
	GRPC_ENFORCE_ALPN_ENABLED=false go run ./cmd/proxy --config=config.yaml

# ── database ──────────────────────────────────────────────────────────────────

DB_DSN ?= postgres://user:pass@localhost:5432/cmk_krypton_bridge?sslmode=disable
DB_CONTAINER = cmk-krypton-bridge-pg

db-up:
	docker run -d --name $(DB_CONTAINER) \
	  -e POSTGRES_DB=cmk_krypton_bridge \
	  -e POSTGRES_USER=user \
	  -e POSTGRES_PASSWORD=pass \
	  -p 5432:5432 \
	  postgres:16-alpine
	@echo "Postgres started — wait a few seconds before connecting."

db-down:
	docker stop $(DB_CONTAINER) && docker rm $(DB_CONTAINER)

db-migrate:
	@echo "Migrations run automatically on proxy startup (goose embedded)."
	@echo "To run manually: DATABASE_DSN='$(DB_DSN)' ./bin/cmk-krypton-bridge --config=config.yaml"

db-reset:
	docker exec $(DB_CONTAINER) psql -U user -d cmk_krypton_bridge \
	  -c "DROP TABLE IF EXISTS goose_db_version, key_hierarchy_jobs;"
	@echo "Schema dropped — next proxy start will re-apply migrations."

# ── demo: real Krypton on worker01 ────────────────────────────────────────────
#
#   make demo                        — default tenant/service
#   make demo TENANT=acme SERVICE=orders
#
# Starts proxy → runs full demo flow → stops proxy.

demo: build
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════╗"
	@echo "║          CmkKryptonBridge POC Demo  (real Krypton backend)       ║"
	@echo "║  Krypton: ing.worker01.kms.shoot.gardener.cc-one.showroom.   ║"
	@echo "║  Tenant : $(TENANT)   Service: $(SERVICE)"
	@echo "╚══════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "▶ Starting proxy..."
	@GRPC_ENFORCE_ALPN_ENABLED=false $(BINARY_PROXY) --config=config.yaml 2>&1 \
	  | grep -v "^$$" & echo $$! > /tmp/cmk-krypton-bridge.pid
	@sleep 1.5
	@echo ""
	@$(MAKE) --no-print-directory demo-health     TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-ensure-l2  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-ensure-l3  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-idempotency TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-ensure-l4  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-list-keys  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@echo "▶ Stopping proxy..."
	@kill $$(cat /tmp/cmk-krypton-bridge.pid) 2>/dev/null || true
	@rm -f /tmp/cmk-krypton-bridge.pid
	@echo ""
	@echo "✓ Demo complete."

# ── demo: mock mode (no external dependencies) ────────────────────────────────

demo-mock: build
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════╗"
	@echo "║       CmkKryptonBridge POC Demo  (in-memory mock mode)       ║"
	@echo "╚══════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "▶ Starting proxy (mock mode)..."
	@$(BINARY_PROXY) --config=config.yaml.mock 2>&1 \
	  | grep -v "^$$" & echo $$! > /tmp/cmk-krypton-bridge.pid
	@sleep 0.8
	@echo ""
	@$(MAKE) --no-print-directory demo-health     TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-ensure-l2  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-ensure-l3  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-idempotency TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-ensure-l4  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-list-keys  TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@echo "▶ Stopping proxy..."
	@kill $$(cat /tmp/cmk-krypton-bridge.pid) 2>/dev/null || true
	@rm -f /tmp/cmk-krypton-bridge.pid
	@echo ""
	@echo "✓ Demo complete (mock mode)."

# ── individual demo steps (proxy must already be running) ─────────────────────

demo-health:
	@echo "┌─ [1/6] Health Check ──────────────────────────────────────────"
	@echo "│  Verifies proxy is up and Krypton backend is reachable."
	@echo "│"
	$(CLI) health
	@echo "└───────────────────────────────────────────────────────────────"

demo-ensure-l2:
	@echo "┌─ [2/6] Ensure L2 — Tenant Root Key ──────────────────────────"
	@echo "│  tenant=$(TENANT)"
	@echo "│  Creates the tenant in Krypton (if new) and announces a K0"
	@echo "│  (tenant-root) key. Idempotent — safe to call repeatedly."
	@echo "│"
	$(CLI) ensure-hierarchy --tenant-id=$(TENANT) --level=L2
	@echo "└───────────────────────────────────────────────────────────────"

demo-ensure-l3:
	@echo "┌─ [3/6] Ensure L3 — Service Key ───────────────────────────────"
	@echo "│  tenant=$(TENANT)  service=$(SERVICE)"
	@echo "│  Recursively ensures L2 exists first, then creates K1 (KEK)."
	@echo "│  created_keys shows what was newly created this call."
	@echo "│"
	$(CLI) ensure-hierarchy --tenant-id=$(TENANT) --service-id=$(SERVICE) --level=L3
	@echo "└───────────────────────────────────────────────────────────────"

demo-idempotency:
	@echo "┌─ [4/6] Idempotency Check ─────────────────────────────────────"
	@echo "│  Same L3 call a second time — created_keys must be empty []."
	@echo "│  Same key ID returned, no duplicate creation."
	@echo "│"
	$(CLI) ensure-hierarchy --tenant-id=$(TENANT) --service-id=$(SERVICE) --level=L3
	@echo "└───────────────────────────────────────────────────────────────"

demo-ensure-l4:
	@echo "┌─ [5/6] Ensure L4 — DEK (Data Encryption Key) ────────────────"
	@echo "│  tenant=$(TENANT)  service=$(SERVICE)"
	@echo "│  Full chain: ensures L2 → L3 → creates K2 (DEK)."
	@echo "│"
	$(CLI) ensure-hierarchy --tenant-id=$(TENANT) --service-id=$(SERVICE) --level=L4
	@echo "└───────────────────────────────────────────────────────────────"

demo-list-keys:
	@echo "┌─ [6/6] List Keys ─────────────────────────────────────────────"
	@echo "│  All keys for tenant=$(TENANT) — shows full hierarchy."
	@echo "│"
	$(CLI) list-keys --tenant-id=$(TENANT)
	@echo "└───────────────────────────────────────────────────────────────"

demo-new-tenant:
	@echo "┌─ New Tenant Demo ─────────────────────────────────────────────"
	@echo "│  Creates a brand-new tenant + full L2→L3 hierarchy in one call."
	@echo "│  tenant=$(TENANT2)  service=$(SERVICE)"
	@echo "│"
	$(CLI) ensure-hierarchy --tenant-id=$(TENANT2) --service-id=$(SERVICE) --level=L3
	@echo "└───────────────────────────────────────────────────────────────"

# ── docker / helm ─────────────────────────────────────────────────────────────

docker:
	docker build -t $(IMAGE_TAG) .

helm-package:
	helm package charts/cmk-krypton-bridge

buf-init:
	buf config init
