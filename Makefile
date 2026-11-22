# Database migrations. Pick another service with SERVICE=notif, for example
SERVICE ?= wallet
DB_URL ?= postgres://payments:payments@localhost:5432/$(SERVICE)?sslmode=disable
MIGRATE := migrate -path ./db/$(SERVICE)/migrations -database "$(DB_URL)"

.PHONY: migrate-up migrate-down migrate-version migrate-new

migrate-up:
	@$(MIGRATE) up

migrate-down:
	@$(MIGRATE) down 1

migrate-version:
	@$(MIGRATE) version

migrate-new:
	@test -n "$(name)" || (echo "usage: make migrate-new name=seq_name" && exit 1)
	migrate-new -ext sql -dir ./db/$(SERVICE)/migrations -seq $(name)
