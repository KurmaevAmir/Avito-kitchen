//go:build integration

// Интеграционные тесты репозитория поверх реального Postgres через
// testcontainers. Запускать: `go test -tags=integration ./internal/repository/`.
// В CI гоняются под отдельной job — на dev-машине без Docker пакет
// компилируется, но тесты пропускаются.
package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kurmaevamir/avito-kitchen/internal/domain"
)

var sharedPool *pgxpool.Pool

// TestMain поднимает один Postgres-контейнер на весь пакет и применяет
// миграции 001 + 002. Если Docker недоступен — пакет SkipAll.
func TestMain(m *testing.M) {
	ctx := context.Background()
	c, err := pgcontainer.Run(ctx, "postgres:16-alpine",
		pgcontainer.WithDatabase("kitchen"),
		pgcontainer.WithUsername("kitchen"),
		pgcontainer.WithPassword("kitchen"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		// Docker недоступен или Docker Hub упал — не валим тесты, а
		// сообщаем и выходим успешно. В CI Docker всегда есть, тесты
		// прогонятся.
		os.Stderr.WriteString("testcontainers: пропускаем integration-тесты — " + err.Error() + "\n")
		os.Exit(0)
	}
	defer func() { _ = testcontainers.TerminateContainer(c) }()

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		os.Stderr.WriteString("dsn: " + err.Error() + "\n")
		os.Exit(1)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		os.Stderr.WriteString("sql.Open: " + err.Error() + "\n")
		os.Exit(1)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		os.Stderr.WriteString("goose dialect: " + err.Error() + "\n")
		os.Exit(1)
	}
	migrationsDir, _ := filepath.Abs("../../migrations")
	if err := goose.Up(db, migrationsDir); err != nil {
		os.Stderr.WriteString("goose up: " + err.Error() + "\n")
		os.Exit(1)
	}
	_ = db.Close()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		os.Stderr.WriteString("pgxpool: " + err.Error() + "\n")
		os.Exit(1)
	}
	sharedPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func setup(t *testing.T) *pgxpool.Pool {
	if sharedPool == nil {
		t.Skip("Docker недоступен — integration тесты пропущены")
	}
	return sharedPool
}

// seedRestaurantAndMenu возвращает id заведения и два его пункта меню.
func seedRestaurantAndMenu(t *testing.T, pool *pgxpool.Pool) (rid, item1, item2 uuid.UUID) {
	ctx := context.Background()
	rid = uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO restaurants (id, name, description, address, is_open, api_key_hash)
		 VALUES ($1, 'test', '', 'a', TRUE, $2)`,
		rid, uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("seed restaurant: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO menu_items (restaurant_id, name, description, price, composition, allergens, available)
		 VALUES ($1, 'X', '', 10000, '', ARRAY[]::TEXT[], TRUE) RETURNING id`,
		rid,
	).Scan(&item1); err != nil {
		t.Fatalf("seed item1: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO menu_items (restaurant_id, name, description, price, composition, allergens, available)
		 VALUES ($1, 'Y', '', 20000, '', ARRAY[]::TEXT[], TRUE) RETURNING id`,
		rid,
	).Scan(&item2); err != nil {
		t.Fatalf("seed item2: %v", err)
	}
	return
}

func TestCreateOrder_HappyPath_SnapshotsPrice(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	pool := setup(t)
	repo := New(pool)
	rid, item1, _ := seedRestaurantAndMenu(t, pool)

	ctx := context.Background()
	o, existed, err := repo.CreateOrder(ctx, CreateOrderInput{
		UserID:          "u1",
		RestaurantID:    rid,
		DeliveryAddress: "addr",
		IdempotencyKey:  "k1",
		RequestHash:     "h1",
		Items:           []CreateOrderItem{{MenuItemID: item1, Quantity: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatal("existed=true на fresh")
	}
	if o.TotalPrice != 30000 {
		t.Fatalf("total_price = %d, ожидали 30000", o.TotalPrice)
	}

	// Меняем цену в меню.
	if _, err := pool.Exec(ctx, `UPDATE menu_items SET price = 99999 WHERE id = $1`, item1); err != nil {
		t.Fatal(err)
	}
	// Snapshot в order_items не должен измениться.
	got, err := repo.GetOrder(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Price != 10000 {
		t.Fatalf("snapshot цены сломался: %+v", got.Items)
	}
}

func TestCreateOrder_IdempotencyKey_ReplayReturnsSameOrder(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	pool := setup(t)
	repo := New(pool)
	rid, item1, _ := seedRestaurantAndMenu(t, pool)
	ctx := context.Background()
	in := CreateOrderInput{
		UserID: "u2", RestaurantID: rid, DeliveryAddress: "addr",
		IdempotencyKey: "same-key", RequestHash: "hash-A",
		Items: []CreateOrderItem{{MenuItemID: item1, Quantity: 1}},
	}
	first, _, err := repo.CreateOrder(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	replay, existed, err := repo.CreateOrder(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !existed || replay.ID != first.ID {
		t.Fatalf("replay: existed=%v id=%s (ожидали id=%s)", existed, replay.ID, first.ID)
	}
}

func TestCreateOrder_IdempotencyKey_DifferentHash_Rejects(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	pool := setup(t)
	repo := New(pool)
	rid, item1, _ := seedRestaurantAndMenu(t, pool)
	ctx := context.Background()
	base := CreateOrderInput{
		UserID: "u3", RestaurantID: rid, DeliveryAddress: "addr",
		IdempotencyKey: "reuse-key", RequestHash: "hash-A",
		Items: []CreateOrderItem{{MenuItemID: item1, Quantity: 1}},
	}
	if _, _, err := repo.CreateOrder(ctx, base); err != nil {
		t.Fatal(err)
	}
	base.RequestHash = "hash-B"
	if _, _, err := repo.CreateOrder(ctx, base); err == nil {
		t.Fatal("ожидали ErrIdempotencyMismatch, получили nil")
	} else if !isIdempotencyMismatch(err) {
		t.Fatalf("ожидали ErrIdempotencyMismatch, получили %v", err)
	}
}

func TestCreateOrder_DuplicateItems_AggregateQuantity(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	pool := setup(t)
	repo := New(pool)
	rid, item1, _ := seedRestaurantAndMenu(t, pool)
	ctx := context.Background()
	o, _, err := repo.CreateOrder(ctx, CreateOrderInput{
		UserID: "u4", RestaurantID: rid, DeliveryAddress: "addr",
		IdempotencyKey: "dup", RequestHash: "h",
		Items: []CreateOrderItem{
			{MenuItemID: item1, Quantity: 1},
			{MenuItemID: item1, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Items) != 1 || o.Items[0].Quantity != 3 {
		t.Fatalf("дубли должны схлопнуться: %+v", o.Items)
	}
	if o.TotalPrice != 30000 {
		t.Fatalf("total_price = %d, ожидали 3*10000", o.TotalPrice)
	}
}

func TestCreateOrder_ClosedRestaurant_Rejects(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	pool := setup(t)
	repo := New(pool)
	rid, item1, _ := seedRestaurantAndMenu(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE restaurants SET is_open = FALSE WHERE id = $1`, rid); err != nil {
		t.Fatal(err)
	}
	_, _, err := repo.CreateOrder(context.Background(), CreateOrderInput{
		UserID: "u5", RestaurantID: rid, DeliveryAddress: "a",
		IdempotencyKey: "k", RequestHash: "h",
		Items: []CreateOrderItem{{MenuItemID: item1, Quantity: 1}},
	})
	if err == nil {
		t.Fatal("ожидали ErrRestaurantClosed")
	}
}

func TestCreateOrder_ConcurrentSameKey_ProducesOneOrder(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	pool := setup(t)
	repo := New(pool)
	rid, item1, _ := seedRestaurantAndMenu(t, pool)
	in := CreateOrderInput{
		UserID: "u-race", RestaurantID: rid, DeliveryAddress: "a",
		IdempotencyKey: "race-key", RequestHash: "same",
		Items: []CreateOrderItem{{MenuItemID: item1, Quantity: 1}},
	}
	const N = 8
	ctx := context.Background()
	ids := make([]uuid.UUID, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			o, _, err := repo.CreateOrder(ctx, in)
			ids[i] = o.ID
			errs[i] = err
		}(i)
	}
	wg.Wait()

	// Все успешные — с одним и тем же ID; допустимо получить и без ошибки.
	var successID uuid.UUID
	for i, err := range errs {
		if err != nil {
			t.Fatalf("i=%d err=%v", i, err)
		}
		if successID == (uuid.UUID{}) {
			successID = ids[i]
			continue
		}
		if ids[i] != successID {
			t.Fatalf("гонка родила два разных заказа: %s vs %s", successID, ids[i])
		}
	}
	// Проверяем в БД: заказ ровно один.
	var count int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE user_id = $1`, in.UserID).Scan(&count)
	if count != 1 {
		t.Fatalf("ожидали 1 заказ, получили %d", count)
	}
}

func TestUpdateOrderStatus_ChangedFlag(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	pool := setup(t)
	repo := New(pool)
	rid, item1, _ := seedRestaurantAndMenu(t, pool)
	ctx := context.Background()
	o, _, err := repo.CreateOrder(ctx, CreateOrderInput{
		UserID: "u6", RestaurantID: rid, DeliveryAddress: "a",
		IdempotencyKey: "s", RequestHash: "h",
		Items: []CreateOrderItem{{MenuItemID: item1, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, changed, err := repo.UpdateOrderStatus(ctx, rid, o.ID, domain.OrderStatusAccepted, "")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("реальный переход должен вернуть changed=true")
	}
	_, changed, err = repo.UpdateOrderStatus(ctx, rid, o.ID, domain.OrderStatusAccepted, "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("no-op должен вернуть changed=false")
	}
}

// isIdempotencyMismatch — вспомогательная проверка (без импорта errors,
// чтобы файл читался как последовательность отдельных тестов).
func isIdempotencyMismatch(err error) bool {
	return err != nil && err.Error() == domain.ErrIdempotencyMismatch.Error()
}
