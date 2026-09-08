-- +goose Up
-- +goose StatementBegin
-- Seed-данные для локальной демонстрации. Не применяется в production
-- (запускать миграции с параметром `-t 1`, чтобы остановиться на 001).
-- В production restaurants создаются админ-процедурой, api_key
-- хранится как хэш (см. миграцию 003).
--
-- api_key_hash соответствует SHA-256 от 'demo-restaurant-key'.

INSERT INTO restaurants (id, name, description, address, is_open, api_key_hash)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    'Демо-кухня',
    'Демо-заведение для сквозного сценария',
    'Москва, Лесная 7',
    TRUE,
    encode(sha256('demo-restaurant-key'::bytea), 'hex')
);

INSERT INTO menu_items (restaurant_id, name, description, price, weight_grams, composition, allergens, available) VALUES
    ('11111111-1111-1111-1111-111111111111', 'Борщ',     'Классический борщ',     35000, 350, 'Свёкла, капуста, картофель, говядина, сметана', ARRAY['celery']::TEXT[], TRUE),
    ('11111111-1111-1111-1111-111111111111', 'Пельмени', 'Домашние пельмени',     42000, 300, 'Тесто, свинина, говядина, лук',                  ARRAY['gluten']::TEXT[], TRUE),
    ('11111111-1111-1111-1111-111111111111', 'Компот',   'Компот из сухофруктов', 15000, 250, 'Сухофрукты, вода, сахар',                        ARRAY[]::TEXT[],        TRUE),
    ('11111111-1111-1111-1111-111111111111', 'Оливье',   'Салат оливье',          29000, 200, 'Картофель, морковь, горошек, огурцы, майонез, ветчина', ARRAY['eggs','mustard']::TEXT[], FALSE);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM menu_items       WHERE restaurant_id = '11111111-1111-1111-1111-111111111111';
DELETE FROM restaurants      WHERE id            = '11111111-1111-1111-1111-111111111111';
-- +goose StatementEnd
