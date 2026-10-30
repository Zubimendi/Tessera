.PHONY: up down test migrate

up:
	docker compose up -d postgres redis

down:
	docker compose down -v

test:
	go test ./...

migrate:
	@echo "Run migrations with your preferred migration tool or psql in the container"
