package testutil

import (
	"context"
	"database/sql"
	"math/rand"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSemaphore(t *testing.T) {
	const limit = 5
	const contenders = 10
	const ttl = 2 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dbPath := filepath.Join(t.TempDir(), "db")

	s, err := newSemaphore(ctx, dbPath, ttl)
	require.NoError(t, err)

	var total int32
	var acquired int32

	doneCh := make(chan struct{})
	for i := 0; i < contenders; i++ {
		go func() {
			// To demonstrate that this Semaphore applies across processes,
			// we'll use multiple instances with the same path. It's not a
			// perfect check.
			s, err := newSemaphore(ctx, dbPath, ttl)
			require.NoError(t, err)

			id, err := s.Acquire(ctx, t.Name(), limit)
			require.NoError(t, err)

			atomic.AddInt32(&acquired, 1)

			<-doneCh

			atomic.AddInt32(&total, 1)
			atomic.AddInt32(&acquired, -1)

			require.NoError(t, s.Release(ctx, id))
		}()
	}

	// Wait for all our above goroutines to become blocked.
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&acquired) == limit
	}, time.Second, 50*time.Millisecond)

	// Assert that TryAcquire returns immediately if s isn't available.
	{
		id, err := s.TryAcquire(ctx, t.Name(), limit)
		require.NoError(t, err)
		require.Nil(t, id)
	}

	// Assert that Acquire respects context cancellation.
	{
		ctx, cancel := context.WithTimeout(ctx, 250*time.Microsecond)
		defer cancel()

		id, err := s.Acquire(ctx, t.Name(), limit)
		require.Equal(t, err, ctx.Err())
		require.Equal(t, id, 0)
	}

	// Slowly unblock all our goroutines, asserting that total continues to
	// increment but acquired stays below limit.
	for i := 0; i < contenders; i++ {
		doneCh <- struct{}{}

		require.Eventually(t, func() bool {
			return int32(i+1) == atomic.LoadInt32(&total) && atomic.LoadInt32(&acquired) <= int32(limit)
		}, time.Second, 50*time.Millisecond)
	}

	require.Equal(t, total, int32(contenders))
}

// TestSQLiteConcurrency is a course test to assert that the SQLite instance
// created by `openDB` provides serializable (maybe linearisable?) consistency
// when `execTX` is used.
func TestSQLiteConcurrency(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "db")

	db, err := openDB(dbPath)
	require.NoError(t, err)

	defer db.Close()

	_, err = db.Exec(`CREATE TABLE counter(n INTEGER NOT NULL);`)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)

		// Fire off N concurrent read + write transactions with their own instance of DB.
		go func() {
			defer wg.Done()

			db, err := openDB(dbPath)
			require.NoError(t, err)

			defer db.Close()

			require.NoError(t, execTX(ctx, db, func(tx *sql.Tx) error {
				var count int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM counter`).Scan(&count); err != nil {
					return err
				}

				// Add a wee bit of chaos to our concurrent writers.
				time.Sleep(time.Duration(rand.Int31n(50)) * time.Microsecond)

				_, err := tx.Exec(`INSERT INTO counter(n) VALUES (?)`, count)
				return err
			}))
		}()
	}

	wg.Wait()

	// If our DB was anything less than serializable, we'd expect to NOT see a
	// smooth count up.
	var result string
	require.NoError(t, db.QueryRow(`SELECT STRING_AGG(n, ',') FROM counter ORDER BY n DESC `).Scan(&result))
	require.Equal(t, "0,1,2,3,4,5,6,7,8,9", result)
}
