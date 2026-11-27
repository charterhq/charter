package registry_test

import (
	"context"
	"encoding/json"
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

// A newer core compiles the same source differently: the version is accepted and recompiled.
// Other source under the same version is refused. A version from before source digests takes its
// first digest once.
func TestVersionIsImmutableBySource(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	a := registry.Agent{Name: "a", Team: "t", Owner: "o"}
	if _, err := registry.Apply(ctx, pool, a, "1.0.0", "sha", []byte(`{"scenarios":{}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApplySource(ctx, pool, a, "1.0.0", "sha", "src1", []byte(`{"scenarios":{},"v":2}`)); err != nil {
		t.Fatalf("legacy version takes its first source digest: %v", err)
	}
	if _, err := registry.ApplySource(ctx, pool, a, "1.0.0", "sha", "src1", []byte(`{"scenarios":{},"v":3}`)); err != nil {
		t.Fatalf("same source, new compiled form: %v", err)
	}
	var pkg string
	_ = pool.QueryRow(ctx, `SELECT package::text FROM agent_versions WHERE agent = 'a'`).Scan(&pkg)
	if pkg != `{"v": 3, "scenarios": {}}` {
		t.Fatalf("compiled form not replaced: %s", pkg)
	}
	if _, err := registry.ApplySource(ctx, pool, a, "1.0.0", "sha", "src2", []byte(`{"scenarios":{},"v":3}`)); !errors.Is(err, registry.ErrVersionChanged) {
		t.Fatalf("other source must be refused, got %v", err)
	}
}

// Objects live across versions: a new version may add state fields, not remove or retype them.
func TestObjectSchemaOnlyGrows(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	a := registry.Agent{Name: "builder", Team: "t", Owner: "o"}
	pkg := func(schema string) []byte {
		b, _ := json.Marshal(map[string]any{"scenarios": map[string]any{}, "object": map[string]any{"schema": schema}})
		return b
	}
	v1 := `{"type":"object","properties":{"work":{"type":"string"}}}`
	if _, err := registry.Apply(ctx, pool, a, "1.0.0", "sha", pkg(v1)); err != nil {
		t.Fatal(err)
	}
	grown := `{"type":"object","properties":{"work":{"type":"string","title":"Work"},"trigger":{"type":"string"}}}`
	if _, err := registry.Apply(ctx, pool, a, "1.1.0", "sha", pkg(grown)); err != nil {
		t.Fatalf("adding a field: %v", err)
	}
	for v, schema := range map[string]string{
		"1.2.0": `{"type":"object","properties":{"trigger":{"type":"string"}}}`,
		"1.3.0": `{"type":"object","properties":{"work":{"type":"array"},"trigger":{"type":"string"}}}`,
	} {
		if _, err := registry.Apply(ctx, pool, a, v, "sha", pkg(schema)); !errors.Is(err, registry.ErrSchemaChanged) {
			t.Fatalf("%s: want ErrSchemaChanged, got %v", v, err)
		}
	}
	if _, err := registry.Apply(ctx, pool, a, "2.0.0", "sha", []byte(`{"scenarios":{}}`)); !errors.Is(err, registry.ErrSchemaChanged) {
		t.Fatalf("dropping spec.object: %v", err)
	}
}
