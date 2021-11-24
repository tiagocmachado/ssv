package history

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/proto"
	sync2 "github.com/bloxapp/ssv/ibft/sync"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// findHighestInstance returns the highest found decided signed message and the peer it was received from
func (s *Sync) findHighestInstance() (*proto.SignedMessage, string, error) {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	usedPeers, err := sync2.GetPeers(s.network, s.publicKey, 4)
	if err != nil {
		return nil, "", err
	}

	results := s.getHighestDecidedFromPeers(usedPeers)

	// no decided msgs were received from peers, return error
	if len(results) == 0 {
		s.logger.Debug("could not fetch highest decided from peers",
			zap.String("identifier", hex.EncodeToString(s.identifier)))
		return nil, "", errors.New("could not fetch highest decided from peers")
	}

	// find the highest decided within the incoming messages
	var ret *proto.SignedMessage
	var fromPeer string
	for _, res := range results {
		if res.Error == kv.EntryNotFoundError {
			continue
		}

		if ret == nil {
			ret = res.SignedMessages[0]
			fromPeer = res.FromPeerID
		}
		if ret.Message.SeqNumber < res.SignedMessages[0].Message.SeqNumber {
			ret = res.SignedMessages[0]
			fromPeer = res.FromPeerID
		}
	}

	// highest decided is a nil msg, meaning no decided found from peers. This can happen if no previous decided instance exists.
	if ret == nil {
		return nil, "", nil
	}

	// found a valid highest decided
	return ret, fromPeer, nil
}

// getHighestDecidedFromPeers receives highest decided messages from peers
func (s *Sync) getHighestDecidedFromPeers(peers []string) []*network.SyncMessage {
	var results []*network.SyncMessage
	var wg sync.WaitGroup
	var lock sync.Mutex

	// peer's highest decided message will be added to results if:
	//  1. not-found-error (i.e. no history)
	//  3. message is valid
	for i, p := range peers {
		wg.Add(1)
		go func(index int, peer string, wg *sync.WaitGroup) {
			defer wg.Done()
			res, err := s.network.GetHighestDecidedInstance(peer, &network.SyncMessage{
				Type:   network.Sync_GetHighestType,
				Lambda: s.identifier,
			})
			if err != nil {
				s.logger.Error("received error when fetching highest decided", zap.Error(err),
					zap.String("identifier", hex.EncodeToString(s.identifier)), zap.Any("peer", peer))
				return
			}
			if len(res.Error) > 0 {
				// assuming not found is a valid scenario (e.g. new validator)
				// therefore we count the result now, and it will be identified afterwards in findHighestInstance()
				if res.Error == kv.EntryNotFoundError {
					lock.Lock()
					results = append(results, res)
					lock.Unlock()
				} else {
					s.logger.Error("received error when fetching highest decided", zap.Error(err),
						zap.String("identifier", hex.EncodeToString(s.identifier)), zap.String("peer", peer))
				}
				return
			}

			if len(res.SignedMessages) != 1 {
				s.logger.Debug("received multiple signed messages", zap.Error(err),
					zap.String("identifier", hex.EncodeToString(s.identifier)))
				return
			}
			if err := s.validateDecidedMsgF(res.SignedMessages[0]); err != nil {
				s.logger.Debug("received invalid highest decided", zap.Error(err),
					zap.String("identifier", hex.EncodeToString(s.identifier)))
				return
			}

			lock.Lock()
			results = append(results, res)
			lock.Unlock()
		}(i, p, &wg)
	}
	wg.Wait()
	s.logger.Debug("got highest decided msg's from peers", zap.Any("count", len(results)))
	return results
}
