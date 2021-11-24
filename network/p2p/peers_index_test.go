package p2p

import (
	"context"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

func TestNewPeersIndex(t *testing.T) {
	ctx := context.Background()
	ua := "test:0.0.0:xxx"

	host1, pi1 := newHostWithPeersIndex(ctx, t, ua+"1")
	host2, pi2 := newHostWithPeersIndex(ctx, t, ua+"2")

	require.NoError(t, host1.Connect(context.TODO(), peer.AddrInfo{
		ID:    host2.ID(),
		Addrs: host2.Addrs(),
	}))

	wait(1 * time.Second)

	pi1.Run()
	pi2.Run()

	t.Run("non exist peer", func(t *testing.T) {
		require.Equal(t, "", pi1.GetPeerData("xxx", UserAgentKey))
	})

	t.Run("non exist property", func(t *testing.T) {
		require.Equal(t, "", pi1.GetPeerData(host2.ID().String(), "xxx"))
	})

	t.Run("get other peers data", func(t *testing.T) {
		// get peer 2 data from peers index 1
		require.Equal(t, ua+"2", pi1.GetPeerData(host2.ID().String(), UserAgentKey))
		// get peer 1 data from peers index 2
		require.Equal(t, ua+"1", pi2.GetPeerData(host1.ID().String(), UserAgentKey))
	})
}

func newHostWithPeersIndex(ctx context.Context, t *testing.T, ua string) (host.Host, PeersIndex) {
	host, err := libp2p.New(ctx,
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.UserAgent(ua))
	require.NoError(t, setupMdnsDiscovery(ctx, zap.L(), host))
	require.NoError(t, err)
	ids, err := identify.NewIDService(host, identify.UserAgent(ua))
	require.NoError(t, err)
	pi := NewPeersIndex(host, ids, zap.L())

	return host, pi
}

func wait(duration time.Duration) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// sleep to allow connection setup
		time.Sleep(duration)

	}()
	wg.Wait()
}
