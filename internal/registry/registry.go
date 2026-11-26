// Package registry keeps agents, their package versions and the kill switch.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Agent identifies an agent and its owners.
type Agent struct {
	Name  string
	Team  string
	Owner string
}

// Apply stores a compiled package version without its source digest (tests, old callers).
func Apply(ctx context.Context, pool *pgxpool.Pool, a Agent, version, gitSHA string, pkg json.RawMessage) (string, error) {
	return ApplySource(ctx, pool, a, version, gitSHA, "", pkg)
}

// ApplySource stores a package version and makes it current for new runs. Runs already started keep
// the version they started on.
//
// A version is immutable by its source: the same version with the same source files is accepted
// again, and its compiled form is replaced when a newer core compiles it differently. Other source
// under the same version is refused. A version stored before source digests existed accepts its
// first digest once; from then on it is checked.
func ApplySource(ctx context.Context, pool *pgxpool.Pool, a Agent, version, gitSHA, sourceDigest string, pkg json.RawMessage) (string, error) {
	var canon any
	if err := json.Unmarshal(pkg, &canon); err != nil {
		return "", fmt.Errorf("package is not JSON: %w", err)
	}
	b, err := json.Marshal(canon)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO agents (name, team, owner) VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE SET team = excluded.team, owner = excluded.owner`, a.Name, a.Team, a.Owner); err != nil {
		return "", err
	}
	id := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent_versions (id, agent, version, git_sha, digest, source_digest, package, applied_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (agent, version) DO UPDATE
		SET applied_at = now(), package = excluded.package, digest = excluded.digest,
		    source_digest = CASE WHEN excluded.source_digest <> '' THEN excluded.source_digest ELSE agent_versions.source_digest END
		WHERE CASE
		    WHEN excluded.source_digest <> '' AND agent_versions.source_digest <> ''
		        THEN agent_versions.source_digest = excluded.source_digest
		    WHEN excluded.source_digest <> '' THEN true -- stored before source digests: takes the first one
		    ELSE agent_versions.digest = excluded.digest
		END
		RETURNING id`, id, a.Name, version, gitSHA, hex.EncodeToString(sum[:]), sourceDigest, b).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A version is immutable: the same number with other content would silently keep the old package.
			return "", fmt.Errorf("%w: %s %s", ErrVersionChanged, a.Name, version)
		}
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id.String(), nil
}

// ErrVersionChanged means the version is already applied with different content.
var ErrVersionChanged = errors.New("version already applied with different content, bump the version")

// Suspend switches an agent off (connection "" means the whole agent). Takes effect for new runs at once.
func Suspend(ctx context.Context, pool *pgxpool.Pool, agent, connection, by, reason string, byPlatform bool) error {
	state := "off_owner"
	if byPlatform {
		state = "off_platform"
	}
	return setSwitch(ctx, pool, agent, connection, state, by, reason)
}

// Resume switches an agent back on.
func Resume(ctx context.Context, pool *pgxpool.Pool, agent, connection, by, reason string) error {
	return setSwitch(ctx, pool, agent, connection, "on", by, reason)
}

func setSwitch(ctx context.Context, pool *pgxpool.Pool, agent, connection, state, by, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO switches (agent, connection, state, reason, changed_by) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (agent, connection) DO UPDATE
		SET state = excluded.state, reason = excluded.reason, changed_by = excluded.changed_by, changed_at = now()`,
		agent, connection, state, reason, by); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]string{"state": state, "reason": reason, "connection": connection})
	if _, err := tx.Exec(ctx, `INSERT INTO audit (actor, action, target, details) VALUES ($1, 'switch', $2, $3)`,
		by, agent, details); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
