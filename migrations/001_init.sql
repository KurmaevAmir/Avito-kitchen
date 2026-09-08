-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Заведения
CREATE TABLE restaurants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    address      TEXT NOT NULL,
    is_open      BOOLEAN NOT NULL DEFAULT TRUE,
    -- SHA-256 (hex, 64 символа) от bearer-токена партнёра. Plaintext-токен
    -- в БД не хранится: дамп БД не даёт доступа к partner-API. При
    -- аутентификации сервис хэширует поступивший токен и ищет по этой
    -- колонке. Rotate = INSERT нового hash + пометить старый deleted_at.
    api_key_hash TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- soft-delete: 152-ФЗ / налоговое законодательство требуют хранить связанные
    -- с заказами данные; вместо физического удаления помечаем deleted_at.
    deleted_at   TIMESTAMPTZ
);

COMMENT ON COLUMN restaurants.api_key_hash IS 'SHA-256 (hex) от bearer-токена партнёра.';

CREATE UNIQUE INDEX restaurants_api_key_hash_alive_uq
    ON restaurants(api_key_hash) WHERE deleted_at IS NULL;

-- Индексы для чтения витрины и админского фильтра по открытости.
CREATE INDEX restaurants_alive_open_idx
    ON restaurants(is_open) WHERE deleted_at IS NULL;

-- Позиции меню
CREATE TABLE menu_items (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    restaurant_id UUID NOT NULL REFERENCES restaurants(id) ON DELETE RESTRICT,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    -- Хранение денег: BIGINT в наименьшей единице (копейках) — предотвращает
    -- ошибки округления. Клиент делит на 100 при показе.
    price         BIGINT NOT NULL CHECK (price >= 0),
    -- Состав/характеристики позиции.
    weight_grams  INT,                      -- вес в граммах, если применимо
    composition   TEXT NOT NULL DEFAULT '', -- ингредиенты одной строкой
    allergens     TEXT[] NOT NULL DEFAULT '{}', -- список аллергенов (SN, gluten, ...)
    -- available — «стоп-лист» (сегодня закончилось), НЕ то же самое, что удаление.
    available     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- soft-delete: партнёр «убрал позицию из меню навсегда». Связанные
    -- order_items ссылаются на неё, но у них уже есть snapshot имени и цены.
    deleted_at    TIMESTAMPTZ
);

COMMENT ON COLUMN menu_items.price IS 'Цена в копейках (наименьшая единица RUB).';
COMMENT ON COLUMN menu_items.available IS 'Стоп-лист: временно недоступно. Не путать с deleted_at.';
COMMENT ON COLUMN menu_items.deleted_at IS 'Мягкое удаление: позиция убрана из меню навсегда.';

CREATE INDEX menu_items_restaurant_alive_available_idx
    ON menu_items(restaurant_id)
    WHERE deleted_at IS NULL AND available = TRUE;

CREATE INDEX menu_items_restaurant_alive_idx
    ON menu_items(restaurant_id) WHERE deleted_at IS NULL;

-- Заказы (иммутабельны, soft-delete не нужен: DELETE-эндпоинтов нет)
CREATE TYPE order_status AS ENUM (
    'created', 'accepted', 'cooking', 'ready', 'delivered', 'cancelled'
);

CREATE TABLE orders (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          TEXT NOT NULL,
    restaurant_id    UUID NOT NULL REFERENCES restaurants(id) ON DELETE RESTRICT,
    status           order_status NOT NULL DEFAULT 'created',
    -- Сумма заказа в копейках (snapshot суммы позиций на момент создания).
    total_price      BIGINT NOT NULL CHECK (total_price >= 0),
    delivery_address TEXT NOT NULL,
    idempotency_key  TEXT NOT NULL,
    -- SHA-256 (hex) от канонизированного тела запроса. По стандарту
    -- IETF idempotency-header тот же ключ с другим телом = 409, а не
    -- «тихо вернуть старый заказ». Сравнение по этому полю не даёт
    -- клиенту получить чужой ответ.
    request_hash     TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, idempotency_key)
);

COMMENT ON COLUMN orders.total_price IS 'Сумма заказа в копейках (snapshot на момент создания).';

CREATE INDEX orders_user_created_idx ON orders(user_id, created_at DESC);
-- Композит с created_at DESC покрывает partner-запрос
-- WHERE restaurant_id=? AND status=? ORDER BY created_at DESC — без in-memory sort.
CREATE INDEX orders_restaurant_status_created_idx ON orders(restaurant_id, status, created_at DESC);

CREATE TABLE order_items (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id     UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    menu_item_id UUID NOT NULL REFERENCES menu_items(id) ON DELETE RESTRICT,
    name         TEXT NOT NULL,  -- snapshot имени
    price        BIGINT NOT NULL CHECK (price >= 0), -- snapshot цены в копейках
    quantity     INTEGER NOT NULL CHECK (quantity > 0)
);

COMMENT ON COLUMN order_items.price IS 'Цена в копейках на момент создания заказа (snapshot).';

CREATE INDEX order_items_order_idx ON order_items(order_id);

CREATE TABLE order_status_history (
    id         BIGSERIAL PRIMARY KEY,
    order_id   UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    status     order_status NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX order_status_history_order_idx
    ON order_status_history(order_id, changed_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS order_status_history;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TYPE  IF EXISTS order_status;
DROP TABLE IF EXISTS menu_items;
DROP TABLE IF EXISTS restaurants;
-- +goose StatementEnd
