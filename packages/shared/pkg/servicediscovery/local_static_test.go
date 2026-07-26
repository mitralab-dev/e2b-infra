package servicediscovery

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/e2b-dev/infra/packages/shared/pkg/consts"
)

func TestNewLocal_ListGivesEachNodeItsOwnID(t *testing.T) {
	t.Parallel()

	d, err := NewLocal("10.0.0.1, 10.0.0.2")
	require.NoError(t, err)

	items, err := d.ListInstances(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 2)
	// The API keys its instance pool by WorkloadID, so duplicate IDs would
	// collapse the nodes into one entry and half the fleet would never receive
	// a sandbox.
	require.Equal(t, "10.0.0.1:"+strconv.Itoa(int(consts.OrchestratorAPIPort)), items[0].WorkloadID)
	require.Equal(t, items[0].WorkloadID, items[0].NodeID)
	require.NotEqual(t, items[0].WorkloadID, items[1].WorkloadID)
	require.Equal(t, "10.0.0.1", items[0].IPAddress)
	require.Equal(t, "10.0.0.2", items[1].IPAddress)
	for _, item := range items {
		require.Equal(t, consts.OrchestratorAPIPort, item.Port)
		require.Equal(t, BackendLocal, item.Backend)
	}
}

func TestNewLocal_ListExplicitPort(t *testing.T) {
	t.Parallel()

	d, err := NewLocal("10.0.0.1:6000,10.0.0.2")
	require.NoError(t, err)

	items, err := d.ListInstances(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "10.0.0.1", items[0].IPAddress)
	require.EqualValues(t, 6000, items[0].Port)
	require.Equal(t, "10.0.0.2", items[1].IPAddress)
	require.Equal(t, consts.OrchestratorAPIPort, items[1].Port)
}

func TestNewLocal_ListSkipsEmptyEntries(t *testing.T) {
	t.Parallel()

	d, err := NewLocal("10.0.0.1,,  ,10.0.0.2")
	require.NoError(t, err)

	items, err := d.ListInstances(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 2)

	_, err = NewLocal(" , ")
	require.Error(t, err)
}

func TestNewLocal_SingleEntryWithNoiseKeepsLocalIdentity(t *testing.T) {
	t.Parallel()

	// The upgrade path: a control plane configured with one node, possibly with a
	// stray comma or spaces, must keep the exact identity the previous line gave it.
	for _, addr := range []string{"10.0.0.1,", " 10.0.0.1 ", ",10.0.0.1:6000"} {
		d, err := NewLocal(addr)
		require.NoError(t, err, addr)

		items, err := d.ListInstances(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1, addr)
		require.Equal(t, "local", items[0].WorkloadID, addr)
		require.Equal(t, "local", items[0].NodeID, addr)
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
