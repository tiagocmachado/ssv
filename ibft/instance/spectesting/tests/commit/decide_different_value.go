package commit

import (
	ibft2 "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/instance/spectesting"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

// DecideDifferentValue tests that a different commit value can be accepted than the prepared value.
// This is a byzantine behaviour by 2/3 of the nodes as the iBFT protocol dictates broadcasting a commit
// message with the prepared value
// TODO - should we allow this?
type DecideDifferentValue struct {
	instance   *ibft2.Instance
	inputValue []byte
	lambda     []byte
}

// Name returns test name
func (test *DecideDifferentValue) Name() string {
	return "pre-prepare -> prepare -> try to commit with different value"
}

// Prepare prepares test
func (test *DecideDifferentValue) Prepare(t *testing.T) {
	test.lambda = []byte{1, 2, 3, 4}
	test.inputValue = spectesting.TestInputValue()

	test.instance = spectesting.TestIBFTInstance(t, test.lambda)
	test.instance.State().Round.Set(1)

	// load messages to queue
	for _, msg := range test.MessagesSequence(t) {
		test.instance.MsgQueue.AddMessage(&network.Message{
			SignedMessage: msg,
			Type:          network.NetworkMsg_IBFTType,
		})
	}
}

// MessagesSequence includes all test messages
func (test *DecideDifferentValue) MessagesSequence(t *testing.T) []*proto.SignedMessage {
	return []*proto.SignedMessage{
		spectesting.PrePrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),

		spectesting.PrepareMsg(t, spectesting.TestSKs()[0], test.lambda, test.inputValue, 1, 1),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[1], test.lambda, test.inputValue, 1, 2),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[2], test.lambda, test.inputValue, 1, 3),
		spectesting.PrepareMsg(t, spectesting.TestSKs()[3], test.lambda, test.inputValue, 1, 4),

		spectesting.CommitMsg(t, spectesting.TestSKs()[0], test.lambda, []byte("wrong value"), 1, 1),
		spectesting.CommitMsg(t, spectesting.TestSKs()[1], test.lambda, []byte("wrong value"), 1, 2),
		spectesting.CommitMsg(t, spectesting.TestSKs()[2], test.lambda, []byte("wrong value"), 1, 3),
		spectesting.CommitMsg(t, spectesting.TestSKs()[3], test.lambda, []byte("wrong value"), 1, 4),
	}
}

// Run runs the test
func (test *DecideDifferentValue) Run(t *testing.T) {
	// pre-prepare
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	// non qualified prepare quorum
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	quorum, _ := test.instance.PrepareMessages.QuorumAchieved(1, test.inputValue)
	require.False(t, quorum)
	// qualified prepare quorum
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	quorum, _ = test.instance.PrepareMessages.QuorumAchieved(1, test.inputValue)
	require.True(t, quorum)
	// non qualified commit quorum
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	quorum, _ = test.instance.CommitMessages.QuorumAchieved(1, test.inputValue)
	require.False(t, quorum)
	// qualified commit quorum
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	spectesting.RequireReturnedTrueNoError(t, test.instance.ProcessMessage)
	quorum, _ = test.instance.CommitMessages.QuorumAchieved(1, []byte("wrong value"))
	require.True(t, quorum)

	require.EqualValues(t, proto.RoundState_Decided, test.instance.State().Stage.Get())
}
