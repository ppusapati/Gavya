package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/domain"
)

// A nullable foreign key written as an empty string is a foreign key to a row
// whose id is "", and no amount of reading the Go code shows that: it is what
// PostgreSQL does with the statement. So these run it.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...

const testDatabase = "catalog_repo_test"

func testRepo(t *testing.T) Repository {
	t.Helper()
	tpl := os.Getenv("TEST_DATABASE_DSN")
	if tpl == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, fmt.Sprintf(tpl, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	defer admin.Close()

	var exists bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, testDatabase).Scan(&exists); err != nil {
		t.Fatalf("look for %s: %v", testDatabase, err)
	}
	if !exists {
		if _, err := admin.Exec(ctx, `CREATE DATABASE "`+testDatabase+`"`); err != nil {
			t.Fatalf("create %s: %v", testDatabase, err)
		}
	}

	pool, err := pgxpool.New(ctx, fmt.Sprintf(tpl, testDatabase))
	if err != nil {
		t.Fatalf("connect to %s: %v", testDatabase, err)
	}
	t.Cleanup(pool.Close)

	schema, err := os.ReadFile(filepath.Join("..", "db", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return New(pool, testIDs{})
}

// testIDs supplies the identifiers audit entries carry. The sequence is enough
// here: the audit chain does not care what the id is, only that it is unique.
type testIDs struct{}

func (testIDs) New() string { return newTestID("AU") }

var idSeq atomic.Int64

func newTestID(prefix string) string {
	body := fmt.Sprintf("%s%d%d", prefix, time.Now().UnixNano(), idSeq.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + "0000000000000000000000000"[:26-len(body)]
}

func product(tenant string) *domain.Product {
	return &domain.Product{
		ID:          newTestID("prd"),
		TenantID:    tenant,
		Name:        "Toned milk",
		Slug:        newTestID("slug"),
		ProductType: "milk",
		Status:      "active",
		CreatedBy:   newTestID("usr"),
		UpdatedBy:   newTestID("usr"),
	}
}

// Category and brand are optional and the columns are nullable, so a product
// with neither is a product with no category — not a rejected request.
func TestAProductWithNoCategoryOrBrandIsCreated(t *testing.T) {
	repo := testRepo(t)
	p := product(newTestID("tnt"))

	out, err := repo.CreateProduct(context.Background(), p)
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if out.CategoryID != "" || out.BrandID != "" {
		t.Errorf("category = %q, brand = %q; want both empty", out.CategoryID, out.BrandID)
	}
}

// The defect: an empty string was written to a nullable foreign key column,
// which is a reference to a row whose id is "". It violated the constraint, and
// the caller was told the service had failed internally.
func TestAnEmptyCategoryIsAbsenceRatherThanABrokenReference(t *testing.T) {
	repo := testRepo(t)
	p := product(newTestID("tnt"))
	p.CategoryID = ""
	p.BrandID = ""

	if _, err := repo.CreateProduct(context.Background(), p); err != nil {
		t.Fatalf("an empty category was treated as a reference: %v", err)
	}
}

// A category that was named but does not exist is the caller's mistake, and
// they need to be told which kind of mistake it is.
func TestAnUnknownCategoryIsNamedRatherThanReportedAsAFailure(t *testing.T) {
	repo := testRepo(t)
	p := product(newTestID("tnt"))
	p.CategoryID = newTestID("gone")

	_, err := repo.CreateProduct(context.Background(), p)
	if !errors.Is(err, ErrUnknownReference) {
		t.Fatalf("err = %v, want ErrUnknownReference", err)
	}
}

func TestAProductWithARealCategoryKeepsIt(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	tenant := newTestID("tnt")

	category, err := repo.CreateCategory(ctx, &domain.Category{
		ID:        newTestID("cat"),
		TenantID:  tenant,
		Name:      "Liquid milk",
		Slug:      newTestID("cslug"),
		CreatedBy: newTestID("usr"),
		UpdatedBy: newTestID("usr"),
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	p := product(tenant)
	p.CategoryID = category.ID
	out, err := repo.CreateProduct(ctx, p)
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if out.CategoryID != category.ID {
		t.Errorf("category = %q, want %q", out.CategoryID, category.ID)
	}
}

func TestASKUForAProductThatDoesNotExistIsNamed(t *testing.T) {
	repo := testRepo(t)

	_, err := repo.CreateSKU(context.Background(), &domain.SKU{
		ID:        newTestID("sku"),
		TenantID:  newTestID("tnt"),
		ProductID: newTestID("gone"),
		Code:      newTestID("code"),
		Name:      "1L pouch",
		Price:     money.Money{Value: 5400, Scale: 2, Currency: "INR"},
		Unit:      "L",
		Status:    "active",
		CreatedBy: newTestID("usr"),
		UpdatedBy: newTestID("usr"),
	})
	if !errors.Is(err, ErrUnknownReference) {
		t.Fatalf("err = %v, want ErrUnknownReference", err)
	}
}

// ListProducts filtered with a LIKE pattern against a column compared for
// equality, so it always came back empty. An empty product_type must mean
// "any", not "none".
func TestListProductsTreatsAnEmptyTypeAsAny(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	tenant := newTestID("tnt")

	if _, err := repo.CreateProduct(ctx, product(tenant)); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	all, err := repo.ListProducts(ctx, tenant, "", "active")
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d products with no type filter, want 1", len(all))
	}

	matching, err := repo.ListProducts(ctx, tenant, "milk", "active")
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(matching) != 1 {
		t.Errorf("got %d products of type milk, want 1", len(matching))
	}

	other, err := repo.ListProducts(ctx, tenant, "ghee", "active")
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("got %d products of type ghee, want 0", len(other))
	}
}
