package ublk

import (
	"testing"

	"github.com/e2b-dev/ublk-go/ublk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A nil backend makes ublk.New fail before it touches the kernel, so any call that slips past the
// closed check surfaces as a different error instead of creating a device.
func TestDevicePoolNewAfterShutdownFails(t *testing.T) {
	t.Parallel()

	p, err := NewDevicePool(0)
	require.NoError(t, err)
	require.NoError(t, p.Shutdown(t.Context()))

	for range 100 {
		dev, err := p.New(t.Context(), nil, 4096)
		require.ErrorIs(t, err, errPoolClosed)
		assert.Nil(t, dev)
	}
	assert.Empty(t, p.sem)
}

func TestDevicePoolCloseUnknownDeviceKeepsSlot(t *testing.T) {
	t.Parallel()

	p, err := NewDevicePool(1)
	require.NoError(t, err)
	p.sem <- struct{}{}

	require.NoError(t, p.Close(t.Context(), &ublk.Device{}))
	assert.Len(t, p.sem, 1)
}
