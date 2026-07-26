package discovery

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/e2b-dev/infra/packages/shared/pkg/consts"
)

func TestStaticOrchestratorHosts_SingleKeepsLocalIdentifier(t *testing.T) {
	t.Parallel()

	items := staticOrchestratorHosts("10.0.0.1")

	require.Len(t, items, 1)
	// The single-host case must stay byte for byte what it was before the list
	// support, so an existing control plane keeps its node identity on upgrade.
	require.Equal(t, "local", items[0].UniqueIdentifier)
	require.Equal(t, "local", items[0].NodeID)
	require.Equal(t, "10.0.0.1", items[0].LocalIPAddress)
	require.Equal(t, consts.OrchestratorAPIPort, items[0].LocalInstanceApiPort)
}

func TestStaticOrchestratorHosts_ListGivesEachNodeItsOwnID(t *testing.T) {
	t.Parallel()

	items := staticOrchestratorHosts("10.0.0.1, 10.0.0.2")

	require.Len(t, items, 2)
	// The API keys its node pool by NodeID, so duplicate IDs would collapse the
	// nodes into one entry and half the fleet would never receive a sandbox.
	require.NotEqual(t, items[0].NodeID, items[1].NodeID)
	require.Equal(t, "10.0.0.1", items[0].LocalIPAddress)
	require.Equal(t, "10.0.0.2", items[1].LocalIPAddress)
	for _, item := range items {
		require.Equal(t, consts.OrchestratorAPIPort, item.LocalInstanceApiPort)
	}
}

func TestStaticOrchestratorHosts_ExplicitPort(t *testing.T) {
	t.Parallel()

	items := staticOrchestratorHosts("10.0.0.1:6000,10.0.0.2")

	require.Len(t, items, 2)
	require.Equal(t, "10.0.0.1", items[0].LocalIPAddress)
	require.EqualValues(t, 6000, items[0].LocalInstanceApiPort)
	require.Equal(t, "10.0.0.2", items[1].LocalIPAddress)
	require.Equal(t, consts.OrchestratorAPIPort, items[1].LocalInstanceApiPort)
}

func TestStaticOrchestratorHosts_SkipsEmptyEntries(t *testing.T) {
	t.Parallel()

	require.Empty(t, staticOrchestratorHosts(""))
	require.Len(t, staticOrchestratorHosts("10.0.0.1,,  ,10.0.0.2"), 2)
}
