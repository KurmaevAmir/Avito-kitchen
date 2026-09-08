# Авито.Кухня — тестовое задание (осень 2026)

MVP backend для сервиса заказа еды, включающий API для клиента, API для заведений
и отдельный демо-сервис заведения, поднимаемый вместе с основным
приложением через Docker Compose.

Как запустить:

```bash
make up
```

Первый запуск создаст `.env` из `.env.example`, соберёт образы и
поднимет весь стек. API: <http://localhost:8080>, OpenAPI:
`/openapi.yaml`. Прогон E2E-смоука: `make smoke`.

---

## Список вопросов и пояснения

### Вопрос 1

ТЗ говорит: «Аутентификацию и авторизацию пользователей реализовывать
не нужно», но заказы должны привязываться к пользователю, а партнёр
должен как-то доказывать, что он это он.

---

В соответствии с задачей я могу принимать решения самостоятельно,
поэтому пользователь идентифицируется заголовком `X-User-Id`
(доверяем), а партнёр — стандартным `Authorization: Bearer <api_key>`.
В БД лежит только SHA-256 hex-хэш ключа (`restaurants.api_key_hash`),
plaintext-токен нигде не хранится. Bearer, а не собственный
`X-Api-Key`, — потому что это RFC 6750: любой HTTP-инструмент
(Postman, Swagger UI, `curl -u`) понимает его без настройки, а
последующая миграция на JWT будет просто сменой валидатора при неизменном
header'е.

---

### Вопрос 2

Как отменить заказ? В ТЗ описан только линейный жизненный цикл
`created → delivered`, но витрина без отмены не бывает.

---

Добавил `POST /v1/orders/{id}/cancel`. Разрешён только пока заказ в
`created` или `accepted` — дальше отмена в компетенции заведения
(логистика уже запущена). Проверка живёт в state-machine статусов.
`GET /v1/orders/{id}/history` из таблицы `order_status_history` 
— клиент показывает таймлайн заказа без дополнительных запросов.

---

### Вопрос 3

Что с идемпотентностью, если клиент прислал тот же `Idempotency-Key`
с другим телом? По умолчанию либо сервер молча возвращает старый
заказ, либо откатывает запрос без объяснения.

---

Реализовал IETF-совместимую семантику. В `orders` добавил колонку
`request_hash` (SHA-256 канонизированного тела). Тот же ключ и тот же
hash, то 200 (OK) и старый заказ. Тот же ключ и другой hash, отдаем 409
`idempotency_key_reuse`. Канонизация решает проблему: `[{X,1},{X,2}]`
и `[{X,3}]` — это один и тот же заказ, а не "клиент прислал другое".

---

### Вопрос 4

Решение проблемы цен. `REAL` даёт классическое `0.1 + 0.2 = 0.30000...4`,
`NUMERIC` требует decimal-библиотеки в Go.

---

`BIGINT` в копейках. Поле называется просто `price`, единица зафиксирована на 
уровне схемы через `COMMENT ON COLUMN menu_items.price IS 'Цена в копейках'` 
и в описании поля в OpenAPI. Клиент делит на 100 при показе. Тот же паттерн, 
что у Stripe с полем `amount`.

---

### Вопрос 5

Что удалять физически, а что через soft-delete?

---

По 152-ФЗ и налоговому кодексу связанные с заказами данные хранятся
годами. Ввёл `deleted_at TIMESTAMPTZ` в `restaurants` и `menu_items`
там есть операции удаления. `UNIQUE(api_key_hash)` заменён на partial
`UNIQUE(...) WHERE deleted_at IS NULL`, чтобы токен "удалённого"
партнёра можно было переиспользовать. `DELETE /v1/partner/menu/{id}`
идемпотентен, то есть повтор на уже удалённой позиции (204). Таблицы
`orders / order_items / order_status_history` являются иммутабельными, у них
и DELETE-эндпоинтов нет.

---

### Описание
Изначально `restaurant-sample` опрашивал `GET /v1/partner/orders?status=…`
раз в 5 секунд и латентность примерно `interval / 2`. Партнёр бьёт в API
впустую большинство тиков.

В итоговом решении api после `COMMIT` транзакции создания заказа
публикует событие в topic-exchange `avito.kitchen.orders`. Sample
биндит очередь по паттерну `orders.<own_id>.#` и получает только свой
поток — routing key: `orders.<restaurant_id>.created`,
`orders.<restaurant_id>.status.<new_status>`. При десяти заведениях
никто не читает поток соседей.

Событие публикуется только при реальной смене статуса, repository
возвращает `changed bool`, сервис по нему решает. Без этого получался
усиливающийся event-цикл: дубль PATCH, sample реагирует, снова
событие, sample снова готовит.

Если `RABBITMQ_URL` пуст, sample автоматически переходит на polling и demo 
работает без брокера.

Данное решение я посчитал оптимальным, потому что оно real-time,
разгружает API от polling-нагрузки и допускает добавление новых
подписчиков без правки основного сервиса. Publish идёт после commit.

---

## Выбор подхода: OpenAPI

### Подход
Кодогенерация DTO и runtime request-validator middleware.

---

### Описание

`oapi-codegen` генерирует Go-типы из `api/openapi.yaml` в
`internal/api/gen/`; handlers используют только сгенерированные
типы. Любое изменение спеки, ломающее контракт, ломает компиляцию.
В CI после regen проверяется `git diff --exit-code`, забыть обновить
типы после правки спеки не получится.

Помимо сгенерированных типов, `kin-openapi` middleware валидирует каждый 
входящий запрос против спеки в рантайме: `required`, `enum`, 
`minimum/maximum`, типы полей, обязательные заголовки. `quantity: 999` 
при `maximum: 50`уходят в 400 до входа в handler, тело без `restaurant_id` при
`required` возвращает тоже 400. Отключается через `OPENAPI_VALIDATE=false`,
если в prod валидирует API Gateway.

---

## Схема БД

Две миграции ([`migrations/`](migrations/)): схема (`001_init.sql`) и
seed демо-заведения отдельным файлом.

| Таблица | Ключевое решение |
| --- | --- |
| `restaurants` | `api_key_hash` (SHA-256); partial `UNIQUE(api_key_hash) WHERE deleted_at IS NULL` |
| `menu_items` | `available BOOL` (стоп-лист, временно) и `deleted_at` (permanent); `price BIGINT` копеек; `weight_grams / composition / allergens[]` |
| `orders` | ENUM `order_status` на уровне БД; `UNIQUE(user_id, idempotency_key)`; `request_hash` для идемпотентности |
| `order_items` | Snapshot `name` и `price` — изменение меню не переписывает историю |
| `order_status_history` | Отдельная таблица под таймлайн, отдаётся через `GET /v1/orders/{id}/history` |

Индексы под конкретные запросы:
`orders(user_id, created_at DESC)` под "мои заказы",
`orders(restaurant_id, status, created_at DESC)` под очередь партнёра
с сортировкой,
`menu_items(restaurant_id) WHERE deleted_at IS NULL AND available` под
публичное меню (partial).

---

## Архитектура

C4-диаграммы: [context](docs/c4/context.puml) [container](docs/c4/container.puml) 
[component](docs/c4/component.puml). 

CJM пользователя и заведения: [`user`](docs/cjm/user.puml), [`restaurant`](docs/cjm/restaurant.puml).

Внутри api разложено по слоям чистой архитектуры. Интерфейсы
(`Repository`, `Cache`, `Publisher`) объявлены у потребителя, что
`internal/service/interfaces.go`; реализации в отдельных пакетах,
удовлетворяют интерфейсам через Go duck typing. Транспорт занимается
только парсингом wire-формата (UUID, JSON, headers), а вся бизнес-валидация
живёт в сервисе.

---

## Дополнительно

Подключен Redis (кэш витрины — `GET /v1/restaurants[/{id}]/menu`,
TTL 30–60 с, инвалидация из мутирующих ручек) и RabbitMQ (шина
событий, описана выше). Обе интеграции fail-open: сервис работает без
Redis, sample работает без RabbitMQ.

---


Настроены линтер и статический анализ: `golangci-lint v2` с
`errcheck`, `staticcheck`, `revive`, `gocritic`, `gosec`,
`sqlclosecheck`, `bodyclose`, `unparam` и другими. Конфиг:
[`.golangci.yml`](.golangci.yml). 

`make lint`: 0 issues.

---

Реализована кодогенерация DTO из OpenAPI через
[`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen).

---

Реализованы тесты по трём уровням:

- Unit сервиса с моками `Repository/Cache/Publisher`, включающий 16 подтестов
  таблично: все ветки валидации, факт публикации события только при
  реальной смене статуса (`internal/service/orders_test.go`).
- Handler через `httptest` тестирующий 401/400/404 сценарии, CORS preflight,
  не-утечка pg-ошибки в `/readyz` (`internal/transport/http/handlers_test.go`).
- Integration через `testcontainers-go` под tag `//go:build integration`
  — snapshot цены, идемпотентность, гонка на UNIQUE, закрытое заведение, 
  дубликат позиций (`internal/repository/repository_test.go`).

```bash
make test                                              # unit + handler (race + coverage)
go test -tags=integration -race ./internal/repository/ # integration
```

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) прогоняет
всё вместе: lint, regen-diff.

---

Реализован E2E-смоук через JetBrains HTTP Client, включащюий 20 запросов с
client-side assertions в [`api/kitchen.http`](api/kitchen.http). Покрывает
витрину, идемпотентность создания заказа, партнёрский поток, illegal
state-transition (409), неверный Bearer (401, WWW-Authenticate),
soft-delete идемпотентен.

```bash
make smoke
```

---

Реализован `Makefile` с командами: 
`make up / down /
logs / test / lint / smoke / generate / diagrams / psql`. 

При первом `make up` `.env` автоматически создаётся из `.env.example`, если его
нет.

Плюс `make diagrams` рендерит все PlantUML в PNG через официальный
docker-образ `plantuml/plantuml`.
