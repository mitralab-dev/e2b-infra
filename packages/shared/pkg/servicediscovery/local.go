package servicediscovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/e2b-dev/infra/packages/shared/pkg/consts"
)

// localInstanceID is the identity the local backend reports on both facets:
// one process on one notional machine.
const localInstanceID = "local"

// localDiscovery returns statically configured addresses, for local
// development against the darwin dummy orchestrator where neither Nomad nor
// Kubernetes is available, and for a control plane fronting a fixed set of
// orchestrator nodes without a scheduler.
type localDiscovery struct {
	NoSync

	instances []Instance
}

// NewLocal builds a Discoverer that always returns the instances reachable at
// addr, a comma-separated list of "host:port" or "host" entries; when the port
// is omitted, consts.OrchestratorAPIPort is used.
//
// A single entry keeps the "local" identity on both facets, byte for byte, so
// an existing deployment keeps its node identity on upgrade. With more than
// one entry the normalized "host:port" becomes the identity, because the API
// keys its instance pool by WorkloadID: duplicate IDs would collapse the nodes
// into one, and two spellings of the same node would register it twice.
func NewLocal(addr string) (Discoverer, error) {
	entries := strings.Split(addr, ",")
	instances := make([]Instance, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		host, port, err := parseLocalAddress(entry)
		if err != nil {
			return nil, err
		}

		id := net.JoinHostPort(host, strconv.FormatUint(uint64(port), 10))
		instances = append(instances, Instance{
			WorkloadID: id,
			NodeID:     id,
			IPAddress:  host,
			Port:       port,
			Backend:    BackendLocal,
		})
	}

	if len(instances) == 0 {
		return nil, fmt.Errorf("local discovery: empty host in %q", addr)
	}

	if len(instances) == 1 {
		instances[0].WorkloadID = localInstanceID
		instances[0].NodeID = localInstanceID
	}

	return &localDiscovery{instances: instances}, nil
}

func parseLocalAddress(addr string) (string, uint16, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// Allow plain "host" without a port.
		host = addr
		portStr = strconv.FormatUint(uint64(consts.OrchestratorAPIPort), 10)
	}
	if host == "" {
		return "", 0, fmt.Errorf("local discovery: empty host in %q", addr)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", 0, fmt.Errorf("local discovery: invalid port %q: %w", portStr, err)
	}

	return host, uint16(port), nil
}

func (d *localDiscovery) ListInstances(ctx context.Context) ([]Instance, error) {
	_, span := tracer.Start(ctx, "list-local-nodes")
	defer span.End()

	return d.instances, nil
}
