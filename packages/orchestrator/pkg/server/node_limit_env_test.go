//go:build linux

package server

import (
	"context"
	"os"
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/testhelpers/ldtestdata"
	"github.com/stretchr/testify/require"

	"github.com/e2b-dev/infra/packages/shared/pkg/featureflags"
)

//nolint:paralleltest,tparallel // t.Setenv
func TestNodeLimitEnvOverrides(t *testing.T) {
	const (
		startingFlagValue = 7
		runningFlagValue  = 300
	)

	source := ldtestdata.DataSource()
	source.Update(source.Flag(featureflags.MaxStartingInstancesPerNode.Key()).ValueForAll(ldvalue.Int(startingFlagValue)))
	source.Update(source.Flag(featureflags.MaxSandboxesPerNode.Key()).ValueForAll(ldvalue.Int(runningFlagValue)))
	flags, err := featureflags.NewClientWithDatasource(source)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, flags.Close(context.WithoutCancel(t.Context()))) })

	limits := []struct {
		name      string
		env       string
		flagValue int
		limit     func(context.Context, *featureflags.Client) int
	}{
		{name: "starting", env: maxStartingInstancesPerNodeEnv, flagValue: startingFlagValue, limit: startingInstancesLimit},
		{name: "running", env: maxSandboxesPerNodeEnv, flagValue: runningFlagValue, limit: runningSandboxesLimit},
	}

	cases := []struct {
		name     string
		set      bool
		value    string
		override int
	}{
		{name: "unset uses the flag"},
		{name: "one overrides the flag", set: true, value: "1", override: 1},
		{name: "fifty overrides the flag", set: true, value: "50", override: 50},
		{name: "zero falls back to the flag", set: true, value: "0"},
		{name: "negative falls back to the flag", set: true, value: "-1"},
		{name: "garbage falls back to the flag", set: true, value: "abc"},
	}

	for _, l := range limits {
		t.Run(l.name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					t.Setenv(l.env, c.value)
					if !c.set {
						require.NoError(t, os.Unsetenv(l.env))
					}

					want := l.flagValue
					if c.override > 0 {
						want = c.override
					}
					require.Equal(t, want, l.limit(t.Context(), flags))
				})
			}
		})
	}
}
