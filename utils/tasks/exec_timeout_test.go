package tasks

import (
	"context"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecWithTimeout(t *testing.T) {
	logex.Build("test", zap.DebugLevel, nil)
	ctxWithTimeout, cancel := context.WithTimeout(context.TODO(), 10*time.Millisecond)
	defer cancel()
	tests := []struct {
		name          string
		ctx           context.Context
		t             time.Duration
		expectedCount uint32
	}{
		{
			"Cancelled_context",
			ctxWithTimeout,
			20 * time.Millisecond,
			3,
		},
		{
			"Long_function",
			context.TODO(),
			8 * time.Millisecond,
			3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var count uint32
			var stopped sync.WaitGroup

			stopped.Add(1)
			fn := func(stopper Stopper) (interface{}, error) {
				for {
					if stopper.IsStopped() {
						stopped.Done()
						return true, nil
					}
					atomic.AddUint32(&count, 1)
					time.Sleep(2 * time.Millisecond)
				}
			}
			completed, _, err := ExecWithTimeout(test.ctx, fn, test.t)
			stopped.Wait()
			require.False(t, completed)
			require.NoError(t, err)
			require.GreaterOrEqual(t, count, test.expectedCount)
		})
	}
}

func TestExecWithTimeout_ShortFunc(t *testing.T) {
	longExec := func(stopper Stopper) (interface{}, error) {
		time.Sleep(2 * time.Millisecond)
		return true, nil
	}
	completed, res, err := ExecWithTimeout(context.TODO(), longExec, 10*time.Millisecond)
	require.True(t, completed)
	require.True(t, res.(bool))
	require.NoError(t, err)
}
