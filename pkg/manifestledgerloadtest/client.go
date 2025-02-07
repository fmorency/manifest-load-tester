package manifestledgerloadtest

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)
import "github.com/cometbft/cometbft-load-test/pkg/loadtest"

type Params struct {
	Fee      int64
	Amount   int64
	Denom    string
	GasLimit uint64
}

// CosmosClientFactory creates instances of CosmosClient
type CosmosClientFactory struct {
	clientCtx client.Context
	params    Params
}

// CosmosClientFactory implements loadtest.ClientFactory
var _ loadtest.ClientFactory = (*CosmosClientFactory)(nil)

func NewCosmosClientFactory(clientCtx client.Context, params Params) *CosmosClientFactory {
	return &CosmosClientFactory{
		clientCtx: clientCtx,
		params:    params,
	}
}

// CosmosClient is responsible for generating transactions. Only one client
// will be created per connection to the remote Tendermint RPC endpoint, and
// each client will be responsible for maintaining its own state in a
// thread-safe manner.
type CosmosClient struct {
	clientCtx client.Context
	params    Params
}

// CosmosClient implements loadtest.Client
var _ loadtest.Client = (*CosmosClient)(nil)

func (f *CosmosClientFactory) ValidateConfig(cfg loadtest.Config) error {
	// Do any checks here that you need to ensure that the load test
	// configuration is compatible with your client.
	return nil
}

func (f *CosmosClientFactory) NewClient(cfg loadtest.Config) (loadtest.Client, error) {
	return &CosmosClient{
		clientCtx: f.clientCtx,
		params:    f.params,
	}, nil
}

// GenerateTx must return the raw bytes that make up the transaction for your
// ABCI app. The conversion to base64 will automatically be handled by the
// loadtest package, so don't worry about that. Only return an error here if you
// want to completely fail the entire load test operation.
func (c *CosmosClient) GenerateTx() ([]byte, error) {
	txBuilder := c.clientCtx.TxConfig.NewTxBuilder()
	r1, err := c.clientCtx.Keyring.Key("user1")
	if err != nil {
		return nil, fmt.Errorf("failed to get user1 key: %w", err)
	}

	r2, err := c.clientCtx.Keyring.Key("user2")
	if err != nil {
		return nil, fmt.Errorf("failed to get user2 key: %w", err)
	}

	addr1, err := r1.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get address from record 1: %w", err)
	}

	addr2, err := r2.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get address from record 2: %w", err)
	}

	msg1 := banktypes.NewMsgSend(addr1, addr2, types.NewCoins(types.NewInt64Coin(c.params.Denom, c.params.Amount)))
	if msg1 == nil {
		return nil, fmt.Errorf("failed to create message")
	}

	err = txBuilder.SetMsgs(msg1)
	if err != nil {
		return nil, fmt.Errorf("failed to set message: %w", err)
	}

	txBuilder.SetGasLimit(c.params.GasLimit)
	txBuilder.SetFeeAmount(types.NewCoins(types.NewInt64Coin(c.params.Denom, c.params.Fee)))
	txBuilder.SetMemo(randomString(10))

	defaultSignMode, err := authsigning.APISignModeToInternal(c.clientCtx.TxConfig.SignModeHandler().DefaultMode())
	if err != nil {
		return nil, fmt.Errorf("failed to get default sign mode: %w", err)
	}

	r1Pub, err := r1.GetPubKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get public key from record 1: %w", err)
	}

	acc1, err := c.clientCtx.AccountRetriever.GetAccount(c.clientCtx, addr1)
	if err != nil {
		return nil, fmt.Errorf("failed to get account number: %w", err)
	}

	// First round: we gather all the signer infos. We use the "set empty
	// signature" hack to do that.
	// https://github.com/cosmos/cosmos-sdk/blob/6f30de3a41d37a4359751f9d9e508b28fc620697/baseapp/msg_service_router_test.go#L169
	sigV2 := signing.SignatureV2{
		PubKey: r1Pub,
		Data: &signing.SingleSignatureData{
			SignMode:  defaultSignMode,
			Signature: nil,
		},
		Sequence: acc1.GetSequence(),
	}
	err = txBuilder.SetSignatures(sigV2)
	if err != nil {
		return nil, fmt.Errorf("failed to set signature: %w", err)
	}

	r1Local := r1.GetLocal()
	r1PrivAny := r1Local.PrivKey
	if r1PrivAny == nil {
		return nil, fmt.Errorf("private key is nil")
	}

	r1Priv, ok := r1PrivAny.GetCachedValue().(cryptotypes.PrivKey)
	if !ok {
		return nil, fmt.Errorf("failed to cast private key from record 1")
	}

	// Second round: all signer infos are set, so each signer can sign.
	signerData := authsigning.SignerData{
		ChainID:       c.clientCtx.ChainID,
		AccountNumber: acc1.GetAccountNumber(),
		Sequence:      acc1.GetSequence(),
		PubKey:        r1Pub,
	}

	sigV2, err = tx.SignWithPrivKey(
		context.TODO(), defaultSignMode, signerData,
		txBuilder, r1Priv, c.clientCtx.TxConfig, acc1.GetSequence())
	if err != nil {
		return nil, fmt.Errorf("failed to sign with private key: %w", err)
	}

	err = txBuilder.SetSignatures(sigV2)
	if err != nil {
		return nil, fmt.Errorf("failed to set signature: %w", err)
	}

	return c.clientCtx.TxConfig.TxEncoder()(txBuilder.GetTx())
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
