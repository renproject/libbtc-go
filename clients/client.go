package clients

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
)

type UTXO struct {
	TxHash       string
	Amount       int64
	ScriptPubKey string
	Vout         uint32
}

type ClientCore interface {
	// NetworkParams should return the network parameters of the underlying
	// Bitcoin blockchain.
	NetworkParams() *chaincfg.Params

	GetUTXOs(ctx context.Context, address string, limit, confitmations int64) ([]UTXO, error)
	Confirmations(ctx context.Context, txHash string) (int64, error)

	// ScriptFunded checks whether a script is funded.
	ScriptFunded(ctx context.Context, address string, value int64) (bool, int64, error)

	// ScriptRedeemed checks whether a script is redeemed.
	ScriptRedeemed(ctx context.Context, address string, value int64) (bool, int64, error)

	// ScriptSpent checks whether a script is spent.
	ScriptSpent(ctx context.Context, script, spender string) (bool, string, error)

	// PublishTransaction should publish a signed transaction to the Bitcoin
	// blockchain.
	PublishTransaction(ctx context.Context, signedTransaction *wire.MsgTx) error
}

type Client interface {
	ClientCore

	// Balance of the given address on Bitcoin blockchain.
	Balance(ctx context.Context, address string, confirmations int64) (int64, error)

	// FormatTransactionView formats the message and txhash into a user friendly
	// message.
	FormatTransactionView(msg, txhash string) string
}

type client struct {
	ClientCore
}

func NewClient(core ClientCore) Client {
	return &client{core}
}

func (client *client) Balance(ctx context.Context, address string, confirmations int64) (int64, error) {
	utxos, err := client.GetUTXOs(ctx, address, 999999, confirmations)
	if err != nil {
		return 0, err
	}
	var balance int64
	for _, utxo := range utxos {
		balance = balance + utxo.Amount
	}
	return balance, nil
}

func (client *client) FormatTransactionView(msg, txhash string) string {
	switch client.NetworkParams().Name {
	case "mainnet":
		return fmt.Sprintf("%s, transaction can be viewed at https://live.blockcypher.com/btc/tx/%s", msg, txhash)
	case "testnet3":
		return fmt.Sprintf("%s, transaction can be viewed at https://live.blockcypher.com/btc-testnet/tx/%s", msg, txhash)
	default:
		return ""
	}
}
