.PHONY: help env up down logs build test lint tidy fmt psql smoke smoke-http generate diagrams clean

help:
	@echo "Основные команды:"
	@echo "  make up          — собрать образы и поднять весь стек (api + postgres + redis + rabbit + sample)"
	@echo "  make down        — остановить стек и удалить volumes"
	@echo "  make logs        — стримить логи всех контейнеров"
	@echo "  make build       — go build всех бинарников локально"
	@echo "  make test        — go test ./... (с race-детектором)"
	@echo "  make lint        — прогнать golangci-lint (должен быть установлен)"
	@echo "  make smoke       — прогнать E2E-сценарий (нужен 'ijhttp' от JetBrains)"
	@echo "  make smoke-http  — то же самое, что 'smoke' (алиас)"
	@echo "  make generate    — пересгенерировать DTO-типы из api/openapi.yaml через oapi-codegen"
	@echo "  make diagrams    — отрендерить PlantUML-диаграммы (docs/**/*.puml) в PNG через docker"
	@echo "  make psql        — открыть psql-сессию в контейнере postgres"

# Создаёт .env из .env.example, если его нет. .env лежит в .gitignore
# (секретам не место в git), шаблон закоммичен.
env:
	@if [ ! -f .env ]; then \
	  echo "[env] .env отсутствует — копируем из .env.example"; \
	  cp .env.example .env; \
	else \
	  echo "[env] .env уже существует — оставляем как есть"; \
	fi

up: env
	docker compose --env-file .env up --build -d
	@echo "API: http://localhost:$$(grep '^API_HOST_PORT=' .env | cut -d= -f2)  |  OpenAPI: http://localhost:$$(grep '^API_HOST_PORT=' .env | cut -d= -f2)/openapi.yaml"

down:
	docker compose --env-file .env down -v || docker compose down -v

logs:
	docker compose logs -f --tail=100

build:
	go build ./...

test:
	go test ./... -race -count=1

lint:
	golangci-lint run

tidy:
	go mod tidy

fmt:
	gofmt -s -w .
	@command -v goimports >/dev/null && goimports -w . || true

psql:
	docker compose exec postgres psql -U $$(grep '^POSTGRES_USER=' .env | cut -d= -f2) -d $$(grep '^POSTGRES_DB=' .env | cut -d= -f2)

smoke smoke-http:
	@command -v ijhttp >/dev/null || { \
	  echo "ijhttp не найден. Установите JetBrains HTTP Client CLI:"; \
	  echo "  https://www.jetbrains.com/help/idea/http-client-cli.html"; \
	  echo "или откройте 'api/kitchen.http' в вашей IDE (IntelliJ/GoLand/VS Code REST Client)."; \
	  exit 1; \
	}
	ijhttp -e local -v api/http-client.env.json api/kitchen.http

generate:
	@command -v oapi-codegen >/dev/null || { \
	  echo "oapi-codegen не найден. Установите:"; \
	  echo "  go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest"; \
	  exit 1; \
	}
	oapi-codegen -config internal/api/oapi.yaml api/openapi.yaml
	@echo "сгенерировано: internal/api/gen/oapi.gen.go"

# Рендерит все .puml в PNG рядом с исходниками. Использует официальный
# образ plantuml/plantuml — локально не нужны ни Java, ни Graphviz.
diagrams:
	@echo "Рендерю docs/**/*.puml → PNG (через контейнер plantuml/plantuml)"
	docker run --rm -v "$(PWD)/docs:/docs" plantuml/plantuml:latest -tpng -o /docs '/docs/**.puml'
	@find docs -name '*.png' | sort | sed 's/^/  /'

clean:
	rm -rf bin
