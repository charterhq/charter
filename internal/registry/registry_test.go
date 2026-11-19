package registry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/testdb"
)

func TestVersionIsImmutable(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	a := registry.Agent{Name: "a", Team: "t", Owner: "o"}
	if _, err := registry.Apply(ctx, pool, a, "1.0.0", "sha", []byte(`{"scenarios":{}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Apply(ctx, pool, a, "1.0.0", "sha", []byte(`{"scenarios":{}}`)); err != nil {
		t.Fatalf("same content must re-apply: %v", err)
	}
	_, err := registry.Apply(ctx, pool, a, "1.0.0", "sha2", []byte(`{"scenarios":{"x":{}}}`))
	if !errors.Is(err, registry.ErrVersionChanged) {
		t.Fatalf("want ErrVersionChanged, got %v", err)
	}
}
