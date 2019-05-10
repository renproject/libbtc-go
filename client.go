package libbtc

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"
	"github.com/renproject/libbtc-go/clients"
	"github.com/renproject/libbtc-go/errors"
)

type Client interface {
	clients.ClientCore

	// Balance of the given address on Bitcoin blockchain.
	Balance(ctx context.Context, address string, confirmations int64) (int64, error)

	// FormatTransactionView formats the message and txhash into a user friendly
	// message.
	FormatTransactionView(msg, txhash string) string

	// SerializePublicKey serializes the given public key.
	SerializePublicKey(pubKey *btcec.PublicKey) ([]byte, error)

	// PublicKeyToAddress converts the public key to a bitcoin address.
	PublicKeyToAddress(pubKeyBytes []byte) (btcutil.Address, error)

	// SlaveAddress creates an a deterministic unique address that can be spent
	// by the private key correspndong to the given master public key hash
	SlaveAddress(mpkh, nonce []byte) (btcutil.Address, error)

	// SlaveScript creates a deterministic unique script that can be spent by
	// the private key correspndong to the given master public key hash
	SlaveScript(mpkh, nonce []byte) ([]byte, error)

	// UTXOCount returns the number of utxos that can be spent.
	UTXOCount(ctx context.Context, address string, confirmations int64) (int, error)

	// Validate returns whether an address is valid or not
	Validate(address string) error
}

type client struct {
	clients.ClientCore
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

func (client *client) UTXOCount(ctx context.Context, address string, confirmations int64) (int, error) {
	utxos, err := client.GetUTXOs(ctx, address, 999999, confirmations)
	if err != nil {
		return 0, err
	}
	return len(utxos), nil
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

func (client *client) SerializePublicKey(pubKey *btcec.PublicKey) ([]byte, error) {
	net := client.NetworkParams()
	switch net {
	case &chaincfg.MainNetParams:
		return pubKey.SerializeCompressed(), nil
	case &chaincfg.TestNet3Params:
		return pubKey.SerializeUncompressed(), nil
	default:
		return nil, errors.NewErrUnsupportedNetwork(net.Name)
	}
}

func (client *client) PublicKeyToAddress(pubKeyBytes []byte) (btcutil.Address, error) {
	net := client.NetworkParams()
	pubKey, err := btcutil.NewAddressPubKey(pubKeyBytes, net)
	if err != nil {
		return nil, err
	}
	addrString := pubKey.EncodeAddress()
	return btcutil.DecodeAddress(addrString, net)
}

func NewBlockchainInfoClient(network string) (Client, error) {
	core, err := clients.NewBlockchainInfoClientCore(network)
	if err != nil {
		return nil, err
	}
	return &client{core}, nil
}

func NewBitcoinFNClient(host, user, password string) (Client, error) {
	core, err := clients.NewBitcoinFNClientCore(host, user, password)
	if err != nil {
		return nil, err
	}
	return &client{core}, nil
}

func NewMercuryClient(network string) (Client, error) {
	core, err := clients.NewMercuryClientCore(network)
	if err != nil {
		return nil, err
	}
	return &client{core}, nil
}
