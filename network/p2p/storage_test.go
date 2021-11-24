package p2p

import (
	"encoding/hex"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testing"
)

func init() {
	logex.Build("test", zap.DebugLevel, nil)
}

var (
	sk  = "ba03f90c6e2e6d67e4a4682621412ddbafeb6bffdc169df8f2bd31f193f001d4"
	sk2 = "2340652c367bf8d17de1bc0454e6aa73e2eedd4a51686887d98d6b8813e5fb4a"
)

func TestSetupPrivateKey(t *testing.T) {
	tests := []struct {
		name      string
		existKey  string
		passedKey string
	}{
		{
			name:      "key not exist passing nothing", // expected - generate new key
			existKey:  "",
			passedKey: "",
		},
		{
			name:      "key not exist passing key in env", // expected - set the passed key
			existKey:  "",
			passedKey: sk2,
		},
		{
			name:      "key exist passing key in env", // expected - override current key with the passed one
			existKey:  sk,
			passedKey: sk2,
		},
		{
			name:      "key exist passing nothing", // expected - do nothing
			existKey:  sk2,
			passedKey: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := basedb.Options{
				Type:   "badger-memory",
				Logger: logex.Build("test", zapcore.DebugLevel, nil),
				Path:   "",
			}

			db, err := ssvstorage.GetStorageFactory(options)
			require.NoError(t, err)
			defer db.Close()

			p2pStorage := storage{
				db:     db,
				logger: zap.L(),
			}

			if test.existKey != "" { // mock exist key
				privKey, err := gcrypto.HexToECDSA(test.existKey)
				require.NoError(t, err)
				require.NoError(t, p2pStorage.SavePrivateKey(privKey))
				sk, found, err := p2pStorage.GetPrivateKey()
				require.True(t, found)
				require.NoError(t, err)
				require.NotNil(t, sk)

				interfacePriv := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(privKey))
				b, err := interfacePriv.Raw()
				require.NoError(t, err)
				require.Equal(t, test.existKey, hex.EncodeToString(b))
			}

			require.NoError(t, p2pStorage.SetupPrivateKey(test.passedKey))
			privateKey, found, err := p2pStorage.GetPrivateKey()
			require.NoError(t, err)
			require.True(t, found)
			require.NoError(t, err)
			require.NotNil(t, privateKey)

			if test.existKey == "" && test.passedKey == "" { // new key generated
				return
			}
			if test.existKey != "" && test.passedKey == "" { // exist and not passed in env
				interfacePriv := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(privateKey))
				b, err := interfacePriv.Raw()
				require.NoError(t, err)
				require.Equal(t, test.existKey, hex.EncodeToString(b))
				return
			}
			// not exist && passed and exist && passed
			interfacePriv := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(privateKey))
			b, err := interfacePriv.Raw()
			require.NoError(t, err)
			require.Equal(t, test.passedKey, hex.EncodeToString(b))
		})
	}
}
