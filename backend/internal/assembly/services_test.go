package assembly

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/WALLE-AI/uMaaS/backend/internal/config"
)

func TestBuildMonolith(t *testing.T) {
	svcs, err := Build(&config.Config{Topology: config.TopologyMonolith})
	require.NoError(t, err)
	assert.Equal(t, config.TopologyMonolith, svcs.Topology)
}

func TestBuildSplit(t *testing.T) {
	svcs, err := Build(&config.Config{Topology: config.TopologySplit})
	require.NoError(t, err)
	assert.Equal(t, config.TopologySplit, svcs.Topology)
}

func TestBuildRejectsUnknownTopology(t *testing.T) {
	_, err := Build(&config.Config{Topology: "cluster"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown topology")
}

// 这是 S1 最重要的一条保障：要求事务原子性的服务不能被远程化。
//
// 单机跑通的事务语义在切到 split 后会静默降级为最终一致，症状是"偶尔超额扣费"
// ——最难查的一类 bug。装配期拒绝，宁可起不来。
type fakeQuota struct{ local bool }

func (f fakeQuota) RequiresLocalTransaction() bool { return f.local }
func (f fakeQuota) ServiceName() string            { return "quota" }

func TestGuardRejectsRemoteTransactionalService(t *testing.T) {
	s := &Services{Topology: config.TopologySplit}
	err := guardTransactionalList(s, []TransactionalService{fakeQuota{local: true}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota")
	assert.Contains(t, err.Error(), "split topology")
}

func TestGuardAllowsNonTransactionalService(t *testing.T) {
	s := &Services{Topology: config.TopologySplit}
	err := guardTransactionalList(s, []TransactionalService{fakeQuota{local: false}})
	assert.NoError(t, err)
}
