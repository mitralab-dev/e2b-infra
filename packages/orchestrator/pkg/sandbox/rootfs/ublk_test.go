package rootfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/block"
	"github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/nbd/testutils"
	ublkpool "github.com/e2b-dev/infra/packages/orchestrator/pkg/sandbox/ublk"
	"github.com/e2b-dev/infra/packages/shared/pkg/storage/header"
)

func TestUblk_RoundTrip(t *testing.T) {
	t.Parallel()

	const mib = 1024 * 1024

	tests := []struct {
		name       string
		deviceSize int64
		flags      int
		offset     int64
		length     int
	}{
		{name: "write", deviceSize: 5 * mib, flags: os.O_RDWR, length: mib},
		{name: "write at offset", deviceSize: 5 * mib, flags: os.O_RDWR, offset: mib / 2, length: mib / 2},
		{name: "direct 4MiB", deviceSize: 10 * mib, flags: unix.O_DIRECT | unix.O_RDWR, length: 4 * mib},
		{name: "direct 32MiB", deviceSize: 256 * mib, flags: unix.O_DIRECT | unix.O_RDWR, length: 32 * mib},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deviceFile := setupUblkDevice(t, tt.deviceSize, tt.flags)

			// O_DIRECT needs page-aligned buffers, which mmap guarantees and make does not.
			testData := mmapBuffer(t, tt.length)
			_, err := rand.Read(testData)
			require.NoError(t, err, "failed to generate random data")

			n, err := deviceFile.WriteAt(testData, tt.offset)
			require.NoError(t, err, "failed to write data to device")
			require.Equal(t, len(testData), n, "partial write")

			readData := mmapBuffer(t, tt.length)
			n, err = deviceFile.ReadAt(readData, tt.offset)
			require.NoError(t, err, "failed to read data from device")
			require.Equal(t, len(readData), n, "partial read")
			require.Equal(t, testData, readData, "data mismatch")
		})
	}
}

func mmapBuffer(t *testing.T, length int) []byte {
	t.Helper()

	buf, err := unix.Mmap(-1, 0, length, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_ANON)
	require.NoError(t, err, "failed to mmap")
	t.Cleanup(func() {
		unix.Munmap(buf)
	})

	return buf
}

func TestUblkBackend_UnalignedWritePreservesBlockData(t *testing.T) {
	t.Parallel()

	const blockSize = header.RootfsBlockSize
	const size = 2 * blockSize

	backend, overlay := setupUblkBackendForTest(t, size)

	base := bytes.Repeat([]byte{0xAA}, int(blockSize))
	n, err := overlay.WriteAt(base, 0)
	require.NoError(t, err)
	require.Equal(t, len(base), n)

	patch := bytes.Repeat([]byte{0xBB}, 512)
	n, err = backend.WriteAt(patch, 512)
	require.NoError(t, err)
	require.Equal(t, len(patch), n)

	got := make([]byte, blockSize)
	n, err = overlay.ReadAt(context.Background(), got, 0)
	require.NoError(t, err)
	require.Equal(t, len(got), n)

	require.Equal(t, base[:512], got[:512])
	require.Equal(t, patch, got[512:1024])
	require.Equal(t, base[1024:], got[1024:])
}

func TestUblkBackend_UnalignedReadAcrossBlockBoundary(t *testing.T) {
	t.Parallel()

	const blockSize = header.RootfsBlockSize
	const size = 2 * blockSize

	backend, overlay := setupUblkBackendForTest(t, size)

	first := bytes.Repeat([]byte{0x11}, int(blockSize))
	second := bytes.Repeat([]byte{0x22}, int(blockSize))

	n, err := overlay.WriteAt(first, 0)
	require.NoError(t, err)
	require.Equal(t, len(first), n)

	n, err = overlay.WriteAt(second, blockSize)
	require.NoError(t, err)
	require.Equal(t, len(second), n)

	buf := make([]byte, 1024)
	n, err = backend.ReadAt(buf, blockSize-512)
	require.NoError(t, err)
	require.Equal(t, len(buf), n)

	require.Equal(t, bytes.Repeat([]byte{0x11}, 512), buf[:512])
	require.Equal(t, bytes.Repeat([]byte{0x22}, 512), buf[512:])
}

func TestUblkBackend_SerializesRMWOnSameBlock(t *testing.T) {
	t.Parallel()

	const blockSize = header.RootfsBlockSize
	const size = 2 * blockSize

	backend, overlay := setupUblkBackendForTest(t, size)
	guard := newSerializingTestDevice(overlay, blockSize)
	backend.dev = guard

	base := bytes.Repeat([]byte{0xAA}, int(blockSize))
	n, err := overlay.WriteAt(base, 0)
	require.NoError(t, err)
	require.Equal(t, len(base), n)

	firstDone := make(chan error, 1)
	go func() {
		_, err := backend.WriteAt(bytes.Repeat([]byte{0x11}, 512), 0)
		firstDone <- err
	}()

	guard.waitFirstReadEntered(t)

	secondDone := make(chan error, 1)
	go func() {
		_, err := backend.WriteAt(bytes.Repeat([]byte{0x22}, 512), 512)
		secondDone <- err
	}()

	guard.assertNoSecondReadEntry(t)
	guard.releaseFirstRead()

	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)

	got := make([]byte, blockSize)
	n, err = overlay.ReadAt(context.Background(), got, 0)
	require.NoError(t, err)
	require.Equal(t, len(got), n)
	require.Equal(t, bytes.Repeat([]byte{0x11}, 512), got[:512])
	require.Equal(t, bytes.Repeat([]byte{0x22}, 512), got[512:1024])
	require.Equal(t, base[1024:], got[1024:])
	require.Equal(t, int32(2), guard.readCalls.Load())
	guard.assertReadsNotConcurrent(t)
}

func TestUblkBackend_AlignedRoundTrip(t *testing.T) {
	t.Parallel()

	const blockSize = header.RootfsBlockSize
	const size = 4 * blockSize

	backend, _ := setupUblkBackendForTest(t, size)

	data := make([]byte, 2*blockSize)
	_, err := rand.Read(data)
	require.NoError(t, err)

	n, err := backend.WriteAt(data, blockSize)
	require.NoError(t, err)
	require.Equal(t, len(data), n)

	got := make([]byte, len(data))
	n, err = backend.ReadAt(got, blockSize)
	require.NoError(t, err)
	require.Equal(t, len(got), n)
	require.Equal(t, data, got)
}

func TestUblkBackend_AlignedPropagatesDeviceError(t *testing.T) {
	t.Parallel()

	const blockSize = header.RootfsBlockSize
	const size = 4 * blockSize

	backend, overlay := setupUblkBackendForTest(t, size)
	backend.dev = &failingTestDevice{Device: overlay}

	n, err := backend.WriteAt(make([]byte, 2*blockSize), blockSize)
	require.ErrorIs(t, err, errTestDevice)
	require.Zero(t, n)

	// A short read keeps the bytes the device did return.
	got := make([]byte, 2*blockSize)
	n, err = backend.ReadAt(got, blockSize)
	require.ErrorIs(t, err, errTestDevice)
	require.Equal(t, int(blockSize), n)
	require.Equal(t, bytes.Repeat([]byte{0x5A}, int(blockSize)), got[:blockSize])
}

var errTestDevice = errors.New("test device failure")

// failingTestDevice fills half of each read and then fails, and fails every write.
type failingTestDevice struct {
	block.Device
}

func (d *failingTestDevice) ReadAt(_ context.Context, p []byte, _ int64) (int, error) {
	half := len(p) / 2
	copy(p[:half], bytes.Repeat([]byte{0x5A}, half))

	return half, errTestDevice
}

func (d *failingTestDevice) WriteAt([]byte, int64) (int, error) {
	return 0, errTestDevice
}

const ublkControlPath = "/dev/ublk-control"

func setupUblkDevice(t *testing.T, size int64, flags int) *os.File {
	t.Helper()

	const blockSize = header.RootfsBlockSize

	if os.Geteuid() != 0 {
		t.Skip("ublk requires root privileges to run")
	}
	if _, err := os.Stat(ublkControlPath); err != nil {
		t.Skipf("ublk driver not loaded: %v", err)
	}

	emptyDevice, err := testutils.NewZeroDevice(size, blockSize)
	require.NoError(t, err, "failed to create zero device")

	cowCachePath := filepath.Join(os.TempDir(), fmt.Sprintf("test-rootfs.ext4.cow.cache-%s", uuid.New().String()))
	t.Cleanup(func() {
		os.RemoveAll(cowCachePath)
	})

	cache, err := block.NewCache(
		size,
		blockSize,
		cowCachePath,
		false,
	)
	require.NoError(t, err, "failed to create cache")

	overlay := block.NewOverlay(emptyDevice, cache)
	t.Cleanup(func() {
		overlay.Close()
	})

	ctx := context.Background()

	pool, err := ublkpool.NewDevicePool(0)
	require.NoError(t, err, "failed to create ublk pool")
	t.Cleanup(func() {
		pool.Shutdown(context.Background())
	})

	backend := newUblkBackend(ctx, overlay)

	dev, err := pool.New(ctx, backend, uint64(size))
	require.NoError(t, err, "failed to create ublk device")
	t.Cleanup(func() {
		pool.Close(context.Background(), dev)
	})

	devicePath := dev.Path()
	t.Logf("ublk device path: %s", devicePath)

	deviceFile, err := os.OpenFile(devicePath, flags, 0)
	require.NoError(t, err, "failed to open device")
	t.Cleanup(func() {
		deviceFile.Close()
	})

	return deviceFile
}

func setupUblkBackendForTest(t *testing.T, size int64) (*ublkBackend, *block.Overlay) {
	t.Helper()

	const blockSize = header.RootfsBlockSize

	zeroDevice, err := testutils.NewZeroDevice(size, blockSize)
	require.NoError(t, err)

	cachePath := filepath.Join(t.TempDir(), fmt.Sprintf("test-ublk-backend-%s.cache", uuid.New().String()))
	cache, err := block.NewCache(size, blockSize, cachePath, false)
	require.NoError(t, err)

	overlay := block.NewOverlay(zeroDevice, cache)
	t.Cleanup(func() {
		require.NoError(t, overlay.Close())
	})

	return newUblkBackend(context.Background(), overlay), overlay
}

type serializingTestDevice struct {
	block.Device
	blockSize int64

	firstReadEntered   chan struct{}
	releaseFirstReadCh chan struct{}
	secondReadEntered  chan struct{}

	readCalls        atomic.Int32
	readsInFlight    atomic.Int32
	maxReadsInFlight atomic.Int32

	once sync.Once
}

func newSerializingTestDevice(dev block.Device, blockSize int64) *serializingTestDevice {
	return &serializingTestDevice{
		Device:             dev,
		blockSize:          blockSize,
		firstReadEntered:   make(chan struct{}),
		releaseFirstReadCh: make(chan struct{}),
		secondReadEntered:  make(chan struct{}, 1),
	}
}

func (d *serializingTestDevice) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	if len(p) == 0 || int64(len(p)) != d.blockSize || off%d.blockSize != 0 {
		return d.Device.ReadAt(ctx, p, off)
	}

	readNum := d.readCalls.Add(1)
	inFlight := d.readsInFlight.Add(1)
	d.recordMaxInFlight(inFlight)
	defer d.readsInFlight.Add(-1)

	if readNum == 1 {
		d.once.Do(func() { close(d.firstReadEntered) })
		<-d.releaseFirstReadCh
	} else if readNum == 2 {
		select {
		case d.secondReadEntered <- struct{}{}:
		default:
		}
	}

	return d.Device.ReadAt(ctx, p, off)
}

func (d *serializingTestDevice) waitFirstReadEntered(t *testing.T) {
	t.Helper()
	select {
	case <-d.firstReadEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first read to enter")
	}
}

func (d *serializingTestDevice) releaseFirstRead() {
	close(d.releaseFirstReadCh)
}

func (d *serializingTestDevice) assertNoSecondReadEntry(t *testing.T) {
	t.Helper()
	select {
	case <-d.secondReadEntered:
		t.Fatal("second RMW entered device.ReadAt for the same block before first completed")
	case <-time.After(150 * time.Millisecond):
	}
}

func (d *serializingTestDevice) assertReadsNotConcurrent(t *testing.T) {
	t.Helper()
	if d.maxReadsInFlight.Load() > 1 {
		t.Fatalf("expected block reads to be serialized, max in-flight reads = %d", d.maxReadsInFlight.Load())
	}
}

func (d *serializingTestDevice) recordMaxInFlight(v int32) {
	for {
		current := d.maxReadsInFlight.Load()
		if v <= current {
			return
		}
		if d.maxReadsInFlight.CompareAndSwap(current, v) {
			return
		}
	}
}
