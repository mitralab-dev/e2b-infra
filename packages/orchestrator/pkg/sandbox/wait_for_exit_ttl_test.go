//go:build linux

package sandbox

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForExit_TellsAnExpiryFromAFailure(t *testing.T) {
	t.Parallel()

	t.Run("end time already past", func(t *testing.T) {
		t.Parallel()

		sbx := newTestSandboxForExit(time.Now().Add(-time.Second))

		require.ErrorIs(t, sbx.WaitForExit(t.Context()), ErrSandboxTTLExpired)
	})

	t.Run("end time passes while waiting", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			sbx := newTestSandboxForExit(time.Now().Add(time.Minute))

			require.ErrorIs(t, sbx.WaitForExit(t.Context()), ErrSandboxTTLExpired)
		})
	})

	t.Run("grace holds the expiry past the end time", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			sbx := newTestSandboxForExit(start.Add(time.Minute))
			done := make(chan error, 1)
			go func() { done <- sbx.WaitForExitAfter(t.Context(), 2*time.Minute) }()

			time.Sleep(2 * time.Minute)
			synctest.Wait()
			select {
			case err := <-done:
				t.Fatalf("returned before end time plus grace: %v", err)
			default:
			}

			require.ErrorIs(t, <-done, ErrSandboxTTLExpired)
			require.Equal(t, 3*time.Minute, time.Since(start))
		})
	})

	t.Run("an exit already in wins over the expiry", func(t *testing.T) {
		t.Parallel()

		sbx := newTestSandboxForExit(time.Now().Add(-time.Second))
		sbx.exit.SetError(errors.New("firecracker process killed"))

		err := sbx.WaitForExit(t.Context())

		require.Error(t, err)
		require.NotErrorIs(t, err, ErrSandboxTTLExpired)
	})

	t.Run("firecracker failure is not an expiry", func(t *testing.T) {
		t.Parallel()

		sbx := newTestSandboxForExit(time.Now().Add(time.Minute))
		sbx.exit.SetError(errors.New("firecracker process killed"))

		err := sbx.WaitForExit(t.Context())

		require.Error(t, err)
		require.NotErrorIs(t, err, ErrSandboxTTLExpired)
	})
}
