package testutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

const semaphoreDDL = `
CREATE TABLE IF NOT EXISTS leases(name TEXT NOT NULL, expires_at INT NOT NULL);
`

var getSemaphore = sync.OnceValues(func() (*semaphore, error) {
	dbPath := filepath.Join(os.TempDir(), "rp-op-tests.sqlite")
	return newSemaphore(context.Background(), dbPath, 10*time.Second)
})

// AcquireSemaphore acquires a named semaphore with an initial value of max and
// releases it via t.Cleanup. It can be used to limit resource usages across
// multiple processes. e.g. Limiting the number of k3d instances across go test
// ./...
func AcquireSemaphore(t *testing.T, name string, max int) {
	s, err := getSemaphore()
	require.NoError(t, err)

	ctx := Context(t)

	id, err := s.Acquire(ctx, name, max)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, s.Release(ctx, id))
	})
}

// semaphore provides persistent cross process lease-based named semaphores
// utilizing SQLite as their backend. Maximums are enforced on a per process
// basis, it is up to consumers to provide the same max value across all
// instances.
type semaphore struct {
	mu    sync.Mutex
	db    *sql.DB
	maxes map[string]int

	ttl      time.Duration
	leaseIDs []int
}

func newSemaphore(ctx context.Context, path string, ttl time.Duration) (*semaphore, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}

	if err := execTX(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.Exec(semaphoreDDL)
		return err
	}); err != nil {
		return nil, err
	}

	s := &semaphore{
		db:    db,
		maxes: map[string]int{},
		ttl:   ttl,
	}

	go s.Run(ctx)

	return s, nil
}

// TryAcquire attempts to acquired a semaphore with `name` returning
// immediately if it is not available.
func (l *semaphore) TryAcquire(ctx context.Context, name string, max int) (*int, error) {
	id, _, err := l.tryAcquire(ctx, name, max)
	return id, err
}

// Acquire blocks until the semaphore with `name` becomes available, returning
// the id of the acquired lease. If `max` changes across invocations, Acquire
// panics.
// When the resource in question is no longer require, call Release with the
// returned id.
func (l *semaphore) Acquire(ctx context.Context, name string, max int) (int, error) {
	for {
		id, sleepFor, err := l.tryAcquire(ctx, name, max)
		if err != nil {
			return 0, err
		}

		if id != nil {
			return *id, nil
		}

		select {
		case <-time.After(sleepFor):
			continue
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

func (l *semaphore) Release(ctx context.Context, id int) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var affected int64
	if err := execTX(ctx, l.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM leases WHERE rowid = ?`, id)
		if err != nil {
			return err
		}

		affected, err = res.RowsAffected()
		return err
	}); err != nil {
		return err
	}

	// There's no mechanism to notify lease holders when their lease has been
	// revoked. In practice, this shouldn't happen and likely won't matter if
	// it does but we want to be aware if it starts to happen a lot as that
	// means something strange is happening.
	if affected != 1 {
		log.Printf("lease %d expired before .Release was called", id)
	}

	l.leaseIDs = slices.DeleteFunc(l.leaseIDs, func(i int) bool {
		return i == id
	})

	return nil
}

// Run heartbeats all leases held by this instance and prunes expired leases.
func (l *semaphore) Run(ctx context.Context) {
	runEvery := time.Duration(float32(l.ttl) * 0.5)

	for {
		lastRun := time.Now()

		if err := l.runOnce(ctx); err != nil {
			panic(fmt.Sprintf("%+v\n", err))
		}

		wait := runEvery - time.Since(lastRun)
		if wait < 0 {
			panic("ttl too short")
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (l *semaphore) runOnce(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return execTX(ctx, l.db, func(tx *sql.Tx) error {
		now := time.Now()
		expiry := time.Now().Add(l.ttl)

		// database/sql doesn't natively support slices and variadic arguments
		// have to be place in a []any container. As this is an internal test
		// only helper, just build the query...
		var ids strings.Builder
		for i, id := range l.leaseIDs {
			if i > 0 {
				ids.WriteRune(',')
			}
			fmt.Fprintf(&ids, "%d", id)
		}

		// Update the expiry of
		query := fmt.Sprintf(`UPDATE leases SET expires_at = ? WHERE rowid IN (%s)`, ids.String())

		res, err := tx.ExecContext(ctx, query, Time(expiry))
		if err != nil {
			return errors.WithStack(err)
		}

		affected, err := res.RowsAffected()
		if err != nil {
			return errors.WithStack(err)
		}

		if int(affected) != len(l.leaseIDs) {
			panic("TODO")
		}

		// Remove any expired leases.
		_, err = tx.ExecContext(ctx, `DELETE FROM leases WHERE expires_at < ?`, Time(now))
		return errors.WithStack(err)
	})
}

func (l *semaphore) setMax(name string, max int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if prev, ok := l.maxes[name]; ok && prev != max {
		panic(errors.Newf("max value of %q changed from %d => %d", prev, max))
	}

	l.maxes[name] = max
}

func (l *semaphore) tryAcquire(ctx context.Context, name string, max int) (*int, time.Duration, error) {
	l.setMax(name, max)

	var acquiredID *int
	var sleepFor time.Duration
	err := execTX(ctx, l.db, func(tx *sql.Tx) error {
		sleepFor = 0
		acquiredID = nil

		var leases int
		var expiry *Time

		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), MIN(expires_at) FROM leases WHERE name = ?`, name).Scan(&leases, &expiry); err != nil {
			return errors.WithStack(err)
		}

		// If there's no available capacity, wait until the next lease will
		// expire to re-check. This could probably be optimized by using a
		// sync.Cond that's triggered from Run.
		if leases >= l.maxes[name] {
			sleepFor = time.Until((time.Time)(*expiry))
			return nil
		}

		var id int
		if err := tx.QueryRow(
			`INSERT INTO leases(name, expires_at) VALUES (?, ?) RETURNING rowid`,
			name,
			Time(time.Now().Add(l.ttl)),
		).Scan(&id); err != nil {
			return errors.WithStack(err)
		}

		acquiredID = &id
		return nil
	})

	if acquiredID != nil {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.leaseIDs = append(l.leaseIDs, *acquiredID)
	}

	return acquiredID, sleepFor, err
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("%s?mode=rwc&journal=WAL&_txlock=EXCLUSIVE", path))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	db.SetMaxIdleConns(1)

	return db, nil
}

func execTX(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	return retryBusy(func() error {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelSerializable,
		})
		if err != nil {
			return errors.WithStack(err)
		}
		fnErr := fn(tx)

		if fnErr == nil {
			return errors.WithStack(tx.Commit())
		}

		if err := tx.Rollback(); err != nil {
			return errors.WithStack(err)
		}

		return fnErr
	})
}

func retryBusy(fn func() error) error {
	for retryCount := 0; ; retryCount++ {
		err := fn()

		var sqlErr sqlite3.Error
		if errors.As(err, &sqlErr) && sqlErr.Code == sqlite3.ErrBusy {
			time.Sleep(time.Duration(retryCount) * 50 * time.Millisecond)
			continue
		}

		return err
	}
}

// Time is a wrapper around [time.Time] for usage with with sqlite as a unix
// timestamp.
// NOTE: While go-sqlite3 claims to have support for [time.Time], it's
// difficult to control the underlying representation and it doesn't support
// aggregations.
type Time time.Time

func (v *Time) Scan(value any) error {
	if value == nil {
		v = nil
		return nil
	}
	if asInt, ok := value.(int64); ok {
		*v = (Time)(time.UnixMilli(asInt))
		return nil
	}
	return errors.Newf("invalid value: %v", value)
}

func (v Time) Value() (driver.Value, error) {
	return driver.Value(time.Time(v).UnixMilli()), nil
}
