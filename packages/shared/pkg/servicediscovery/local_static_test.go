package servicediscovery

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/e2b-dev/infra/packages/shared/pkg/consts"
)

func TestNewLocal_ListGivesEachNodeItsOwnID(t *testing.T) {
	t.Parallel()

	d, err := NewLocal("10.0.0.1, 10.0.0.2:6000")
	require.NoError(t, err)

	items, err := d.ListInstances(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "10.0.0.1:"+strconv.Itoa(int(consts.OrchestratorAPIPort)), items[0].WorkloadID)
	require.Equal(t, "10.0.0.2:6000", items[1].WorkloadID)
	require.Equal(t, "10.0.0.1", items[0].IPAddress)
	require.Equal(t, consts.OrchestratorAPIPort, items[0].Port)
	require.Equal(t, "10.0.0.2", items[1].IPAddress)
	require.EqualValues(t, 6000, items[1].Port)
	for _, item := range items {
		require.Equal(t, item.WorkloadID, item.NodeID)
		require.Equal(t, BackendLocal, item.Backend)
	}
}

func TestNewLocal_ListSkipsEmptyEntries(t *testing.T) {
	t.Parallel()

	d, err := NewLocal("10.0.0.1,,  ,10.0.0.2")
	require.NoError(t, err)

	items, err := d.ListInstances(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 2)
}

func TestNewLocal_SingleEntryWithNoiseKeepsLocalIdentity(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{",10.0.0.1:6000,", " 10.0.0.1:6000 , "} {
		d, err := NewLocal(addr)
		require.NoError(t, err, addr)

		items, err := d.ListInstances(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1, addr)
		require.Equal(t, "local", items[0].WorkloadID, addr)
		require.Equal(t, "local", items[0].NodeID, addr)
		require.Equal(t, "10.0.0.1:6000", items[0].Address(), addr)
	}
}

func TestNewLocal_TwoSpellingsOfOneNodeCollapse(t *testing.T) {
	t.Parallel()

	d, err := NewLocal("10.0.0.1,10.0.0.1:" + strconv.Itoa(int(consts.OrchestratorAPIPort)))
	require.NoError(t, err)

	items, err := d.ListInstances(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, items[0].WorkloadID, items[1].WorkloadID, "same node must not register twice")
}

func TestNewLocal_ListRejectsABadEntry(t *testing.T) {
	t.Parallel()

	_, err := NewLocal("10.0.0.1,10.0.0.2:grpc")
	require.Error(t, err)
}
