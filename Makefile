TUSK_DB := postgres://postgres:postgres@localhost:5432/tuskdev?sslmode=disable

.PHONY: build run dev db-up db-down db-reset db-psql workload workload-loop loadtest clean

## Build
build:
	go build -o tusk ./cmd/tusk

## Run tusk against the local dev database
run: build
	./tusk '$(TUSK_DB)'

## Build + run (alias)
dev: run

## Start postgres in docker
db-up:
	docker compose up -d
	@echo "Waiting for postgres..."
	@until docker exec tusk-postgres pg_isready -q; do sleep 0.5; done
	@echo "Postgres ready at localhost:5432"

## Stop postgres
db-down:
	docker compose down

## Nuke and recreate (drops volume)
db-reset:
	docker compose down -v
	$(MAKE) db-up

## Open psql shell
db-psql:
	docker exec -it tusk-postgres psql -U postgres -d tuskdev

## Run the workload script once (generates queries you can watch in tusk)
workload:
	docker exec -i tusk-postgres psql -U postgres -d tuskdev < scripts/workload.sql

## Run workload in a loop (background traffic)
workload-loop:
	@echo "Running workload every 5s — Ctrl+C to stop"
	@while true; do \
		docker exec -i tusk-postgres psql -U postgres -d tuskdev < scripts/workload.sql > /dev/null 2>&1; \
		sleep 5; \
	done

## Full load test — concurrent OLTP, analytics, idle-in-txn, lock contention (default 60s)
loadtest:
	./scripts/loadtest.sh $(or $(DURATION),60)

## Clean build artifacts
clean:
	rm -f tusk
