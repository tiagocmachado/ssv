package p2p

import (
	"context"
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
)

// SubscribeToValidatorNetwork  for new validator create new topic, subscribe and start listen
func (n *p2pNetwork) SubscribeToValidatorNetwork(validatorPk *bls.PublicKey) error {
	n.psTopicsLock.Lock()
	defer n.psTopicsLock.Unlock()

	pubKey := validatorPk.SerializeToHexStr()
	logger := n.logger.With(zap.String("who", "SubscribeToValidatorNetwork"), zap.String("pubKey", pubKey))

	if _, ok := n.cfg.Topics[pubKey]; !ok {
		if err := n.joinTopic(pubKey); err != nil {
			return errors.Wrap(err, "failed to join to topic")
		}
		logger.Debug("joined topic")
	} else {
		logger.Debug("known topic")
	}

	if _, ok := n.psSubs[pubKey]; !ok {
		sub, err := n.cfg.Topics[pubKey].Subscribe()
		if err != nil {
			if err != pubsub.ErrTopicClosed {
				return errors.Wrap(err, "failed to subscribe on Topic")
			}
			// rejoin a topic in case it was closed, and trying to subscribe again
			if err := n.joinTopic(pubKey); err != nil {
				return errors.Wrap(err, "failed to join to topic")
			}
			sub, err = n.cfg.Topics[pubKey].Subscribe()
			if err != nil {
				return errors.Wrap(err, "failed to subscribe on Topic")
			}
		}
		logger.Debug("subscribed to topic")
		ctx, cancel := context.WithCancel(n.ctx)
		n.psSubs[pubKey] = cancel
		go func() {
			topicName := sub.Topic()
			n.listen(ctx, sub)
			// close topic and mark it as not subscribed
			n.psTopicsLock.Lock()
			defer n.psTopicsLock.Unlock()
			if err := n.closeTopic(topicName); err != nil {
				n.logger.Error("failed to close topic", zap.String("topic", topicName), zap.Error(err))
			}
			// make sure the context is canceled once listen was done from some reason
			if cancel, ok := n.psSubs[pubKey]; ok {
				defer cancel()
				delete(n.psSubs, pubKey)
			}
		}()
	} else {
		logger.Debug("subscription exist")
	}

	return nil
}

// AllPeers returns all connected peers for a validator PK (except for the validator itself)
func (n *p2pNetwork) AllPeers(validatorPk []byte) ([]string, error) {
	topic, err := n.getTopic(validatorPk)
	if err != nil {
		return nil, err
	}

	return n.allPeersOfTopic(topic), nil
}

// joinTopic joins to the given topic and mark it in topics map
// this method is not thread-safe - should be called after psTopicsLock was acquired
func (n *p2pNetwork) joinTopic(pubKey string) error {
	topic, err := n.pubsub.Join(getTopicName(pubKey))
	if err != nil {
		return errors.Wrap(err, "failed to join to topic")
	}
	n.cfg.Topics[pubKey] = topic
	return nil
}

// closeTopic closes the given topic
func (n *p2pNetwork) closeTopic(topicName string) error {
	pk := unwrapTopicName(topicName)
	if t, ok := n.cfg.Topics[pk]; ok {
		delete(n.cfg.Topics, pk)
		return t.Close()
	}
	return nil
}

// getTopic return topic by validator public key
func (n *p2pNetwork) getTopic(validatorPK []byte) (*pubsub.Topic, error) {
	n.psTopicsLock.RLock()
	defer n.psTopicsLock.RUnlock()

	if validatorPK == nil {
		return nil, errors.New("ValidatorPk is nil")
	}
	topic := n.fork.ValidatorTopicID(validatorPK)
	if _, ok := n.cfg.Topics[topic]; !ok {
		return nil, errors.New("topic is not exist or registered")
	}
	return n.cfg.Topics[topic], nil
}

// AllPeers returns all connected peers for a validator PK (except for the validator itself and public peers like exporter)
func (n *p2pNetwork) allPeersOfTopic(topic *pubsub.Topic) []string {
	ret := make([]string, 0)

	skippedPeers := map[string]bool{
		n.cfg.ExporterPeerID: true,
	}
	for _, p := range topic.ListPeers() {
		isValidNodeType := validateNodeType(n.peersIndex.GetPeerData, p)
		if s := peerToString(p); !skippedPeers[s] && isValidNodeType {
			ret = append(ret, peerToString(p))
		}
	}

	return ret
}

// listen listens on the given subscription
func (n *p2pNetwork) listen(ctx context.Context, sub *pubsub.Subscription) {
	t := sub.Topic()
	defer sub.Cancel()
	n.logger.Info("start listen to topic", zap.String("topic", t))
	for {
		select {
		case <-ctx.Done():
			n.logger.Info("context is done, subscription will be cancelled", zap.String("topic", t))
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				n.logger.Error("failed to get message from subscription Topics", zap.Error(err))
				return
			}
			n.trace("received raw network msg", zap.ByteString("network.Message bytes", msg.Data))
			cm, err := n.fork.DecodeNetworkMsg(msg.Data)
			if err != nil {
				n.logger.Error("failed to un-marshal message", zap.Error(err))
				continue
			}
			if n.reportLastMsg && len(msg.ReceivedFrom) > 0 {
				reportLastMsg(msg.ReceivedFrom.String())
			}
			n.propagateSignedMsg(cm)
		}
	}
}

// validateNodeType return if peer nodeType is valid.
// checks if peer support nodeType in userAgent. if not, use peer. (backwards compatibility)
func validateNodeType(peerData func(pid string, key string) string, p peer.ID) bool {
	nodeType := getNodeType(peerData, p)

	return nodeType != Exporter.String()
}

func getNodeType(peerData func(pid string, key string) string, p peer.ID) string {
	ua := peerData(p.String(), UserAgentKey)
	uaParts := strings.Split(ua, ":")
	if len(uaParts) > 2 {
		return Unknown.FromString(uaParts[2]).String()
	}
	return Unknown.String()
}

// getTopicName return formatted topic name
func getTopicName(pk string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, pk)
}

// getTopicName return formatted topic name
func unwrapTopicName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}
