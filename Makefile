.PHONY: all build demo-build test test-integration helm-test lint proto docker helm-package run tidy buf-init \
        demo demo-mock demo-async demo-async-mock \
        demo-health demo-ensure-l2 demo-ensure-l3 demo-ensure-l4 \
        demo-idempotency demo-list-keys demo-new-tenant \
        demo-async-enqueue demo-async-watch demo-async-registry \
        demo-orbital demo-orbital-mock demo-orbital-portforward \
        db-up db-down db-migrate db-reset

BINARY_PROXY        = bin/cmk-krypton-bridge
BINARY_CLI          = bin/cmk2kryptonctl
BINARY_KRYPTON_TEST = bin/krypton-test
BINARY_ORBITAL_DEMO = bin/orbital-demo
PROTO_DIR     = proto
GEN_DIR       = internal/gen
IMAGE_TAG     = ghcr.io/openkcm/images/cmk-krypton-bridge:latest
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
	go build -o $(BINARY_PROXY)        ./cmd/cmk-krypton-bridge
	go build -o $(BINARY_CLI)          ./cmd/cli

demo-build: build
	go build -o $(BINARY_KRYPTON_TEST) ./cmd/demos/krypton-test
	go build -o $(BINARY_ORBITAL_DEMO) ./cmd/demos/orbital-demo

tidy:
	go mod tidy

# ── tests ─────────────────────────────────────────────────────────────────────

test:
	go test -race ./...

test-integration:
	go test -tags integration -race ./...

# Helm chart unit tests: render charts/cmk-krypton-bridge with `helm template` and load the
# configuration with the bridge's own code. Needs the helm command, no cluster.
helm-test:
	go test -count=1 -race -tags=helmtests ./helm-tests/unit/...

lint:
	golangci-lint run ./...

# ── run ───────────────────────────────────────────────────────────────────────

run:
	GRPC_ENFORCE_ALPN_ENABLED=false go run ./cmd/cmk-krypton-bridge --config=config.yaml

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
	@$(BINARY_PROXY) --config=config.mock.yaml 2>&1 \
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

# ── demo: async flow with Registry integration ────────────────────────────────
#
#   make demo-async                          — full async demo (real Krypton + Registry)
#   make demo-async-mock                     — async demo in mock mode (no external deps)
#
# Shows: enqueue → job_id returned immediately → poll until DONE → Registry registered.

demo-async: build
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════╗"
	@echo "║       CmkKryptonBridge — Async + Registry Integration Demo       ║"
	@echo "║  Shows: enqueue → ACCEPTED → polling → DONE → Registry sync      ║"
	@echo "╚══════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "▶ Starting proxy (real Krypton + Registry)..."
	@GRPC_ENFORCE_ALPN_ENABLED=false $(BINARY_PROXY) --config=config.yaml 2>&1 \
	  | grep -v "^$$" & echo $$! > /tmp/cmk-krypton-bridge.pid
	@sleep 1.5
	@echo ""
	@$(MAKE) --no-print-directory demo-async-enqueue TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@$(MAKE) --no-print-directory demo-async-registry TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@echo "▶ Stopping proxy..."
	@kill $$(cat /tmp/cmk-krypton-bridge.pid) 2>/dev/null || true
	@rm -f /tmp/cmk-krypton-bridge.pid
	@echo ""
	@echo "✓ Async demo complete."

demo-async-mock: build
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════╗"
	@echo "║   CmkKryptonBridge — Async + Registry Demo  (mock mode)          ║"
	@echo "╚══════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "▶ Starting proxy (mock mode, Registry disabled)..."
	@$(BINARY_PROXY) --config=config.mock.yaml 2>&1 \
	  | grep -v "^$$" & echo $$! > /tmp/cmk-krypton-bridge.pid
	@sleep 0.8
	@echo ""
	@$(MAKE) --no-print-directory demo-async-enqueue TENANT=$(TENANT) SERVICE=$(SERVICE)
	@echo ""
	@echo "▶ Stopping proxy..."
	@kill $$(cat /tmp/cmk-krypton-bridge.pid) 2>/dev/null || true
	@rm -f /tmp/cmk-krypton-bridge.pid
	@echo ""
	@echo "✓ Async demo complete (mock mode)."

demo-async-enqueue:
	@echo "┌─ [1/3] Enqueue Job ───────────────────────────────────────────────"
	@echo "│  Caller gets job_id immediately — Krypton call happens async."
	@echo "│  tenant=$(TENANT)  service=$(SERVICE)  level=L3"
	@echo "│"
	@JOB_ID=$$($(CLI) ensure-hierarchy --tenant-id=$(TENANT) --service-id=$(SERVICE) --level=L3 \
	  | grep '"job_id"' | sed 's/.*"job_id": *"\([^"]*\)".*/\1/'); \
	echo "│  job_id: $$JOB_ID"; \
	echo "│"; \
	echo "│  Status while worker processes..."; \
	echo "└───────────────────────────────────────────────────────────────────"; \
	echo ""; \
	echo "┌─ [2/3] Watch Job Until Done ──────────────────────────────────────"; \
	echo "│  Polling every 2s — worker drives job to completion with retry."; \
	echo "│"; \
	$(CLI) watch-job --job-id=$$JOB_ID --interval=2 --timeout=60; \
	echo "└───────────────────────────────────────────────────────────────────"

demo-async-registry:
	@echo "┌─ [3/3] Verify Registry Sync ──────────────────────────────────────"
	@echo "│  After job DONE, worker registered tenant+system in CMK Registry."
	@echo "│  tenant=$(TENANT)  service=$(SERVICE)"
	@echo "│"
	@echo "│  (Registry list-systems call would show the registered entry here)"
	@echo "│  → In production: check registry.$(TENANT) via cmk-registry-ctl"
	@echo "└───────────────────────────────────────────────────────────────────"

# ── demo: orbital full-flow (mock mode — no external deps) ───────────────────
#
#   make demo-orbital-mock
#
# Shows the complete flow:
#   CMK Backend (orbital-demo) → orbital :9094
#   Bridge orbital handler     → jobstore.Enqueue
#   Worker pool                → mock Krypton client
#   Mock Registry              → logged but no real call
#
# Requires: no Postgres, no Krypton, no Registry — pure in-memory mock.

demo-orbital-mock: demo-build
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════╗"
	@echo "║   CmkKryptonBridge — Full Orbital Flow Demo  (mock mode)         ║"
	@echo "║   CMK→orbital→Bridge→[mock Krypton]→[mock Registry]             ║"
	@echo "╚══════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "▶ Starting proxy (mock mode, orbital enabled)..."
	@$(BINARY_PROXY) --config=config.mock.yaml 2>&1 | grep -v "^$$" & echo $$! > /tmp/cmk-krypton-bridge.pid
	@sleep 0.8
	@echo ""
	@echo "┌─ [1/3] Sending orbital TaskRequest (simulating CMK Backend) ──────"
	@echo "│  task type : cmk-krypton-bridge.ensure-key-hierarchy"
	@echo "│  tenant    : $(TENANT)   service: $(SERVICE)   level: L3"
	@echo "│"
	@$(BINARY_ORBITAL_DEMO) \
	  --orbital-addr=localhost:9094 \
	  --proxy-addr=localhost:9092 \
	  --tenant-id=$(TENANT) \
	  --service-id=$(SERVICE) \
	  --level=3 \
	  --poll-interval=200ms \
	  --timeout=30s
	@echo ""
	@echo "▶ Stopping proxy..."
	@kill $$(cat /tmp/cmk-krypton-bridge.pid) 2>/dev/null || true
	@rm -f /tmp/cmk-krypton-bridge.pid
	@echo ""
	@echo "✓ Orbital demo complete (mock mode)."

# ── demo: orbital full-flow (real Krypton + Registry via port-forward) ───────
#
#   Prerequisites (run in separate terminals first):
#   Terminal 1: kubectl port-forward -n services svc/krypton-root 18080:8080
#   Terminal 2: kubectl port-forward -n services svc/registry 19092:9092
#   Terminal 3: docker run -d -p 5432:5432 -e POSTGRES_DB=cmk_krypton_bridge \
#                 -e POSTGRES_USER=user -e POSTGRES_PASSWORD=pass postgres:16-alpine
#
#   make demo-orbital-portforward

demo-orbital-portforward: demo-build
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════╗"
	@echo "║   CmkKryptonBridge — Orbital Demo  (port-forward to worker01)    ║"
	@echo "║   Krypton : localhost:18080  Registry : localhost:19092           ║"
	@echo "╚══════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "▶ Starting proxy (port-forward config)..."
	@GRPC_ENFORCE_ALPN_ENABLED=false $(BINARY_PROXY) --config=config.portforward.yaml 2>&1 \
	  | grep -v "^$$" & echo $$! > /tmp/cmk-krypton-bridge.pid
	@sleep 1.5
	@echo ""
	@$(BINARY_ORBITAL_DEMO) \
	  --orbital-addr=localhost:9094 \
	  --proxy-addr=localhost:9092 \
	  --tenant-id=$(TENANT) \
	  --service-id=$(SERVICE) \
	  --level=3 \
	  --poll-interval=500ms \
	  --timeout=60s
	@echo ""
	@echo "▶ Stopping proxy..."
	@kill $$(cat /tmp/cmk-krypton-bridge.pid) 2>/dev/null || true
	@rm -f /tmp/cmk-krypton-bridge.pid
	@echo ""
	@echo "✓ Port-forward demo complete."

demo-orbital: demo-build
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════════════╗"
	@echo "║   CmkKryptonBridge — Full Orbital Flow Demo  (real backends)     ║"
	@echo "║   CMK→orbital→Bridge→Krypton→Registry                           ║"
	@echo "╚══════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "▶ Starting proxy (real Krypton + Registry)..."
	@GRPC_ENFORCE_ALPN_ENABLED=false $(BINARY_PROXY) --config=config.yaml 2>&1 \
	  | grep -v "^$$" & echo $$! > /tmp/cmk-krypton-bridge.pid
	@sleep 1.5
	@echo ""
	@$(BINARY_ORBITAL_DEMO) \
	  --orbital-addr=localhost:9094 \
	  --proxy-addr=localhost:9092 \
	  --tenant-id=$(TENANT) \
	  --service-id=$(SERVICE) \
	  --level=3 \
	  --poll-interval=500ms \
	  --timeout=60s
	@echo ""
	@echo "▶ Stopping proxy..."
	@kill $$(cat /tmp/cmk-krypton-bridge.pid) 2>/dev/null || true
	@rm -f /tmp/cmk-krypton-bridge.pid
	@echo ""
	@echo "✓ Orbital demo complete."



docker:
	docker build -t $(IMAGE_TAG) .

helm-package:
	helm package charts/cmk-krypton-bridge

buf-init:
	buf config init
