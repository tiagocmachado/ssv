package ibft

import (
	"context"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/ibft"
	ibftctl "github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/history"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
	"time"
)

// DecidedReaderOptions defines the required parameters to create an instance
type DecidedReaderOptions struct {
	Logger         *zap.Logger
	Storage        collections.Iibft
	Network        network.Network
	Config         *proto.InstanceConfig
	ValidatorShare *storage.Share

	Out *event.Feed
}

// decidedReader reads decided messages history
type decidedReader struct {
	logger  *zap.Logger
	storage collections.Iibft
	network network.Network

	config         *proto.InstanceConfig
	validatorShare *storage.Share

	out *event.Feed

	identifier []byte
}

// newDecidedReader creates new instance of DecidedReader
func newDecidedReader(opts DecidedReaderOptions) Reader {
	r := decidedReader{
		logger: opts.Logger.With(
			zap.String("pubKey", opts.ValidatorShare.PublicKey.SerializeToHexStr()),
			zap.String("ibft", "decided_reader")),
		storage:        opts.Storage,
		network:        opts.Network,
		config:         opts.Config,
		validatorShare: opts.ValidatorShare,
		out:            opts.Out,
		identifier: []byte(format.IdentifierFormat(opts.ValidatorShare.PublicKey.Serialize(),
			beacon.RoleTypeAttester.String())),
	}
	return &r
}

// sync starts to fetch best known decided message (highest sequence) from the network and sync to it.
func (r *decidedReader) sync() error {
	r.logger.Debug("syncing ibft data")
	// creating HistorySync and starts it
	hs := history.New(r.logger, r.validatorShare.PublicKey.Serialize(), r.identifier, r.network,
		r.storage, r.validateDecidedMsg)
	err := hs.Start()
	if err != nil {
		r.logger.Error("could not sync validator's data", zap.Error(err))
	}
	return err
}

// Share returns the reader's share
func (r *decidedReader) Share() *storage.Share {
	return r.validatorShare
}

// Start starts to listen to decided messages
func (r *decidedReader) Start() error {
	if err := r.network.SubscribeToValidatorNetwork(r.validatorShare.PublicKey); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}
	if err := tasks.Retry(func() error {
		if err := r.sync(); err != nil {
			r.logger.Error("could not sync validator", zap.Error(err))
			return err
		}
		return nil
	}, 3); err != nil {
		ibftctl.ReportIBFTStatus(r.validatorShare.PublicKey.SerializeToHexStr(), false, true)
		r.logger.Error("could not setup validator, sync failed", zap.Error(err))
		return err
	}
	ibftctl.ReportIBFTStatus(r.validatorShare.PublicKey.SerializeToHexStr(), true, false)

	r.logger.Debug("sync is done, starting to read network messages")
	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	if err := r.waitForMinPeers(ctx, r.validatorShare.PublicKey, 1); err != nil {
		return errors.Wrap(err, "could not wait for min peers")
	}
	cn, done := r.network.ReceivedDecidedChan()
	defer done()
	r.listenToNetwork(cn)
	return nil
}

func (r *decidedReader) listenToNetwork(cn <-chan *proto.SignedMessage) {
	r.logger.Debug("listening to decided messages")
	for msg := range cn {
		if err := validateMsg(msg, string(r.identifier)); err != nil {
			continue
		}
		logger := r.logger.With(messageFields(msg)...)
		if err := validateDecidedMsg(msg, r.validatorShare); err != nil {
			logger.Debug("received invalid decided message")
			continue
		}
		if msg.Message.SeqNumber == 0 {
			logger.Debug("received invalid sequence")
			continue
		}
		go func(msg *proto.SignedMessage) {
			defer logger.Debug("done with decided msg")
			if saved, err := r.handleNewDecidedMessage(msg); err != nil {
				if !saved {
					logger.Error("could not handle decided message", zap.Error(err))
				}
				logger.Error("could not check highest decided", zap.Error(err))
			}
		}(msg)
	}
}

// handleNewDecidedMessage saves an incoming (valid) decided message
func (r *decidedReader) handleNewDecidedMessage(msg *proto.SignedMessage) (bool, error) {
	logger := r.logger.With(messageFields(msg)...)
	if known, _ := r.checkDecided(msg); known {
		logger.Debug("received known sequence")
		return false, nil
	}
	if err := r.storage.SaveDecided(msg); err != nil {
		return false, errors.Wrap(err, "could not save decided")
	}
	logger.Debug("decided saved")
	ibft.ReportDecided(r.validatorShare.PublicKey.SerializeToHexStr(), msg)
	go r.out.Send(newDecidedAPIMsg(msg, r.validatorShare.PublicKey.SerializeToHexStr()))
	return true, r.checkHighestDecided(msg)
}

// checkDecided check if the new decided message is a duplicate or should override existing message ()
func (r *decidedReader) checkDecided(msg *proto.SignedMessage) (bool, error) {
	decided, found, err := r.storage.GetDecided(r.identifier, msg.Message.SeqNumber)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	// decided message should have at least 3 signers, so if the new decided has 4 signers -> override
	if len(msg.SignerIds) > len(decided.SignerIds) {
		return false, nil
	}
	return true, nil
}

// checkHighestDecided check if highest decided should be updated
func (r *decidedReader) checkHighestDecided(msg *proto.SignedMessage) error {
	logger := r.logger.With(messageFields(msg)...)
	seq := msg.Message.SeqNumber
	highestKnown, found, err := r.storage.GetHighestDecidedInstance(r.identifier)
	if err != nil {
		return errors.Wrap(err, "could not get highest decided")
	}
	if found {
		highestSeqKnown := uint64(0)
		if highestKnown != nil {
			highestSeqKnown = highestKnown.Message.SeqNumber
		}
		if seq < highestSeqKnown {
			logger.Debug("received old sequence",
				zap.Uint64("highestSeqKnown", highestSeqKnown))
			return nil
		}
		if seq > highestSeqKnown+1 {
			if err := r.sync(); err != nil {
				logger.Debug("could not sync", zap.Uint64("seq", seq),
					zap.Uint64("highestSeqKnown", highestSeqKnown))
				return err
			}
			return nil
		}
	}
	if err := r.storage.SaveHighestDecidedInstance(msg); err != nil {
		return errors.Wrap(err, "could not save highest decided")
	}
	logger.Info("highest decided saved")
	return nil
}

// validateDecidedMsg validates the message
func (r *decidedReader) validateDecidedMsg(msg *proto.SignedMessage) error {
	r.logger.Debug("validating a new decided message", zap.String("msg", msg.String()))
	return validateDecidedMsg(msg, r.validatorShare)
}

// waitForMinPeers will wait until enough peers joined the topic
func (r *decidedReader) waitForMinPeers(ctx context.Context, pk *bls.PublicKey, minPeerCount int) error {
	return commons.WaitForMinPeers(commons.WaitMinPeersCtx{
		Ctx:    ctx,
		Logger: r.logger,
		Net:    r.network,
	}, pk.Serialize(), minPeerCount, 1*time.Second, 64*time.Second, false)
}

func validateDecidedMsg(msg *proto.SignedMessage, share *storage.Share) error {
	p := pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.AuthorizeMsg(share),
		auth.ValidateQuorum(share.ThresholdSize()),
	)
	return p.Run(msg)
}

func validateMsg(msg *proto.SignedMessage, identifier string) error {
	p := pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.ValidateLambdas([]byte(identifier)),
	)
	return p.Run(msg)
}

func newDecidedAPIMsg(msg *proto.SignedMessage, pk string) api.Message {
	return api.Message{
		Type: api.TypeDecided,
		Filter: api.MessageFilter{
			PublicKey: pk,
			From:      int64(msg.Message.SeqNumber), To: int64(msg.Message.SeqNumber),
			Role: api.RoleAttester},
		Data: []*proto.SignedMessage{msg},
	}
}
