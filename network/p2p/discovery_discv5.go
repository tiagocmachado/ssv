package p2p

import (
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p/peers/scorers"
	"go.uber.org/zap"
	"net"
	"time"
)

// discv5Listener represents the discv5 interface
type discv5Listener interface {
	Self() *enode.Node
	Close()
	Lookup(enode.ID) []*enode.Node
	Resolve(*enode.Node) *enode.Node
	RandomNodes() enode.Iterator
	Ping(*enode.Node) error
	RequestENR(*enode.Node) (*enode.Node, error)
	LocalNode() *enode.LocalNode
}

// setupDiscV5 creates all the required objects for discv5
func (n *p2pNetwork) setupDiscV5() (*discover.UDPv5, error) {
	n.peers = peers.NewStatus(n.ctx, &peers.StatusConfig{
		PeerLimit: maxPeers,
		ScorerParams: &scorers.Config{
			BadResponsesScorerConfig: &scorers.BadResponsesScorerConfig{
				Threshold:     5,
				DecayInterval: time.Hour,
			},
		},
	})
	ip, err := ipAddr()
	if err != nil {
		return nil, err
	}
	listener, err := n.createListener(ip)
	if err != nil {
		return nil, errors.Wrap(err, "could not create listener")
	}
	record := listener.Self()
	n.logger.Info("Self ENR", zap.String("enr", record.String()))
	return listener, nil
}

// createListener creates a new discv5 listener
func (n *p2pNetwork) createListener(ipAddr net.IP) (*discover.UDPv5, error) {
	var bindIP net.IP
	switch udpVersionFromIP(ipAddr) {
	case udp4:
		bindIP = net.IPv4zero
	case udp6:
		bindIP = net.IPv6zero
	default:
		return nil, errors.New("invalid ip provided")
	}

	//// If Local ip is specified then use that instead.
	//if s.cfg.LocalIP != "" {
	//	ipAddr = net.ParseIP(s.cfg.LocalIP)
	//	if ipAddr == nil {
	//		return nil, errors.New("invalid Local ip provided")
	//	}
	//	bindIP = ipAddr
	//}
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: n.cfg.UDPPort,
	}
	// Listen to all network interfaces
	// for both ip protocols.
	networkVersion := "udp"
	conn, err := net.ListenUDP(networkVersion, udpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}

	localNode, err := n.createExtendedLocalNode(ipAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not create Local node")
	}

	dv5Cfg := discover.Config{
		PrivateKey: n.privKey,
	}
	if n.cfg.NetworkTrace {
		logger := log.New()
		logger.SetHandler(&dv5Logger{n.logger.With(zap.String("who", "dv5Logger"))})
		dv5Cfg.Log = logger
	}
	dv5Cfg.Bootnodes, err = parseENRs(n.cfg.BootnodesENRs, true)
	if err != nil {
		return nil, errors.Wrap(err, "could not read bootstrap addresses")
	}
	// create discv5 listener
	listener, err := discover.ListenV5(conn, localNode, dv5Cfg)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to discV5")
	}
	return listener, nil
}

// createExtendedLocalNode creates an extended enode.LocalNode with all the needed entries to be part of its enr
func (n *p2pNetwork) createExtendedLocalNode(ipAddr net.IP) (*enode.LocalNode, error) {
	operatorPubKey, err := n.getOperatorPubKey()
	if err != nil {
		return nil, err
	}
	localNode, err := createLocalNode(
		n.privKey,
		ipAddr,
		n.cfg.UDPPort,
		n.cfg.TCPPort,
	)
	if err != nil {
		return nil, errors.Wrap(err, "could not create Local node")
	}

	if len(operatorPubKey) > 0 {
		localNode, err = addOperatorPubKeyEntry(localNode, []byte(pubKeyHash(operatorPubKey)))
		if err != nil {
			return nil, errors.Wrap(err, "could not create public key entry")
		}
	}

	// TODO: add fork entry once applicable
	//localNode, err = addForkEntry(localNode, s.genesisTime, s.genesisValidatorsRoot)
	//if err != nil {
	//	return nil, errors.Wrap(err, "could not add eth2 fork version entry to enr")
	//}

	// update local node to use provided host address
	if n.cfg.HostAddress != "" {
		hostIP := net.ParseIP(n.cfg.HostAddress)
		if hostIP.To4() == nil && hostIP.To16() == nil {
			n.logger.Error("Invalid host address given", zap.String("hostIp", hostIP.String()))
		} else {
			n.logger.Info("using external IP", zap.String("IP from config", n.cfg.HostAddress), zap.String("IP", hostIP.String()))
			localNode.SetFallbackIP(hostIP)
			localNode.SetStaticIP(hostIP)
		}
	}
	// update local node to use provided host DNS
	if n.cfg.HostDNS != "" {
		_host := n.cfg.HostDNS
		ips, err := net.LookupIP(_host)
		if err != nil {
			return nil, errors.Wrap(err, "could not resolve host address")
		}
		if len(ips) > 0 {
			// Use first IP returned from the
			// resolver.
			firstIP := ips[0]
			n.logger.Info("using DNS IP", zap.String("DNS", n.cfg.HostDNS), zap.String("IP", firstIP.String()))
			localNode.SetFallbackIP(firstIP)
		}
	}
	return localNode, nil
}

// createLocalNode create a new enode.LocalNode instance
func createLocalNode(privKey *ecdsa.PrivateKey, ipAddr net.IP, udpPort, tcpPort int) (*enode.LocalNode, error) {
	db, err := enode.OpenDB("")
	if err != nil {
		return nil, errors.Wrap(err, "could not open node's peer database")
	}
	localNode := enode.NewLocalNode(db, privKey)

	ipEntry := enr.IP(ipAddr)
	udpEntry := enr.UDP(udpPort)
	tcpEntry := enr.TCP(tcpPort)
	localNode.Set(ipEntry)
	localNode.Set(udpEntry)
	localNode.Set(tcpEntry)
	localNode.SetFallbackIP(ipAddr)
	localNode.SetFallbackUDP(udpPort)

	return localNode, nil
}

// listenForNewNodes watches for new nodes in the network and connects to unknown peers.
func (n *p2pNetwork) listenForNewNodes() {
	defer n.logger.Debug("done listening for new nodes")
	iterator := n.dv5Listener.RandomNodes()
	//iterator = enode.Filter(iterator, s.filterPeer)
	defer iterator.Close()
	n.logger.Debug("starting to listen for new nodes")
	for {
		// Exit if service's context is canceled
		if n.ctx.Err() != nil {
			break
		}
		if n.isPeerAtLimit() {
			n.logger.Debug("at peer limit")
			time.Sleep(6 * time.Second)
			continue
		}
		exists := iterator.Next()
		if !exists {
			break
		}
		node := iterator.Node()
		peerInfo, err := convertToAddrInfo(node)
		if err != nil {
			n.trace("could not convert node to peer info", zap.Error(err))
			continue
		}
		go func(info *peer.AddrInfo) {
			if err := n.connectWithPeer(n.ctx, *info); err != nil {
				n.trace("can't connect with peer", zap.String("peerID", info.ID.String()), zap.Error(err))
			}
		}(peerInfo)
	}
}

// isPeerAtLimit checks for max peers
func (n *p2pNetwork) isPeerAtLimit() bool {
	numOfConns := len(n.host.Network().Peers())
	activePeers := len(n.peers.Active())
	return activePeers >= maxPeers || numOfConns >= maxPeers
}

// dv5Logger implements log.Handler to track logs of discv5
type dv5Logger struct {
	logger *zap.Logger
}

// Log takes a record and uses the zap.Logger to print it
func (dvl *dv5Logger) Log(r *log.Record) error {
	logger := dvl.logger.With(zap.Any("context", r.Ctx))
	switch r.Lvl {
	case log.LvlTrace:
		logger.Debug(r.Msg)
	case log.LvlDebug:
		logger.Debug(r.Msg)
	case log.LvlInfo:
		logger.Info(r.Msg)
	case log.LvlWarn:
		logger.Warn(r.Msg)
	case log.LvlError:
		logger.Error(r.Msg)
	case log.LvlCrit:
		logger.Fatal(r.Msg)
	default:
	}
	return nil
}

// addOperatorPubKeyEntry adds public key entry ('pk') to the node.
// contains the sha256 (hex encoded) of the operator public key
func addOperatorPubKeyEntry(node *enode.LocalNode, pkHash []byte) (*enode.LocalNode, error) {
	bitL, err := bitfield.NewBitlist64FromBytes(64, pkHash)
	if err != nil {
		return node, err
	}
	entry := enr.WithEntry("pk", bitL.ToBitlist())
	node.Set(entry)
	return node, nil
}

// extractOperatorPubKeyEntry extracts the value of public key entry ('pk')
func extractOperatorPubKeyEntry(record *enr.Record) ([]byte, error) {
	bitL := bitfield.NewBitlist(64)
	entry := enr.WithEntry("pk", &bitL)
	err := record.Load(entry)
	if err != nil {
		return nil, err
	}
	return bitL.Bytes(), nil
}
