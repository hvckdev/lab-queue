.PHONY: up down frontend frontend-dev build test test-independent test-ui integration check

GO ?= go
NPM ?= npm
FRONTEND := projects/web/frontend
PACKAGES := ./tests/architecture ./pkg/platform/... ./projects/accounts/... ./projects/queue/... ./projects/web/...
MODULES := pkg/platform projects/accounts projects/queue projects/web

up:
	docker compose up --build -d --wait
down:
	docker compose down
frontend:
	$(NPM) --prefix $(FRONTEND) ci
	$(NPM) --prefix $(FRONTEND) run build
frontend-dev:
	$(NPM) --prefix $(FRONTEND) run dev
build: frontend
	@mkdir -p bin
	$(GO) build -o bin/accounts ./projects/accounts/cmd
	$(GO) build -o bin/queue ./projects/queue/cmd
	$(GO) build -o bin/web ./projects/web/cmd
test:
	$(GO) test $(PACKAGES)
test-independent:
	@set -e; for module in $(MODULES); do (cd "$$module" && GOWORK=off $(GO) test ./...); done
test-ui:
	$(NPM) --prefix $(FRONTEND) test
integration:
	@test -n "$$TEST_ACCOUNTS_DATABASE_URL" -a -n "$$TEST_QUEUE_DATABASE_URL" || (echo 'Set TEST_ACCOUNTS_DATABASE_URL and TEST_QUEUE_DATABASE_URL'; exit 1)
	$(GO) test -race -count=1 $(PACKAGES)

check:
	$(GO) vet $(PACKAGES)
	$(GO) test $(PACKAGES)
