package clients

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

type bitcoinFNClient struct {
	client  *rpcclient.Client
	client2 RPCCLient
	params  *chaincfg.Params
}

func NewBitcoinFNClientCore(host, user, password string) (ClientCore, error) {
	client, err := rpcclient.New(
		&rpcclient.ConnConfig{
			Host:         host,
			User:         user,
			Pass:         password,
			HTTPPostMode: true,
			DisableTLS:   true,
		},
		nil,
	)
	if err != nil {
		return nil, err
	}

	bcInfo, err := client.GetBlockChainInfo()
	if err != nil {
		return nil, err
	}

	var params *chaincfg.Params
	switch bcInfo.Chain {
	case "main":
		params = &chaincfg.MainNetParams
	case "test":
		params = &chaincfg.TestNet3Params
	case "regtest":
		params = &chaincfg.RegressionNetParams
	default:
		return nil, fmt.Errorf("unsupported bitcoin network: %s", bcInfo.Chain)
	}

	return &bitcoinFNClient{
		client:  client,
		client2: NewRPCClient(host, user, password),
		params:  params,
	}, nil
}

func (client *bitcoinFNClient) GetUTXOs(ctx context.Context, address string, limit, confitmations int64) ([]UTXO, error) {
	net := client.NetworkParams()
	addr, err := btcutil.DecodeAddress(address, net)
	if err != nil {
		return []UTXO{}, err
	}

	unspents, err := client.client.ListUnspentMinMaxAddresses(0, 999999, []btcutil.Address{addr})
	if err != nil {
		return []UTXO{}, err
	}

	if len(unspents) == 0 {
		if err := client.client.ImportAddressRescan(address, "", false); err != nil {
			return []UTXO{}, err
		}

		unspents, err = client.client.ListUnspentMinMaxAddresses(0, 999999, []btcutil.Address{addr})
		if err != nil {
			return []UTXO{}, err
		}
	}

	utxos := []UTXO{}
	for _, unspent := range unspents {
		utxos = append(utxos, UTXO{
			TxHash:       unspent.TxID,
			Amount:       int64(unspent.Amount * math.Pow(10, 8)),
			ScriptPubKey: unspent.ScriptPubKey,
			Vout:         unspent.Vout,
		})
	}
	return utxos, nil
}

func (client *bitcoinFNClient) Confirmations(ctx context.Context, txHashStr string) (int64, error) {
	txHash, err := chainhash.NewHashFromStr(txHashStr)
	if err != nil {
		return 0, err
	}
	tx, err := client.client.GetTransaction(txHash)
	if err != nil {
		return 0, err
	}
	return tx.Confirmations, nil
}

func (client *bitcoinFNClient) ScriptFunded(ctx context.Context, address string, value int64) (bool, int64, error) {
	if err := client.client.ImportAddressRescan(address, "scripts", false); err != nil {
		return false, value, err
	}
	net := client.NetworkParams()
	addr, err := btcutil.DecodeAddress(address, net)
	if err != nil {
		return false, value, err
	}
	amount, err := client.client.GetReceivedByAddressMinConf(addr, 0)
	if err != nil {
		return false, value, err
	}
	return int64(amount.ToUnit(btcutil.AmountSatoshi)) >= value, int64(amount.ToUnit(btcutil.AmountSatoshi)), nil
}

func (client *bitcoinFNClient) ScriptRedeemed(ctx context.Context, address string, value int64) (bool, int64, error) {
	if err := client.client.ImportAddressRescan(address, "scripts", false); err != nil {
		return false, value, err
	}
	net := client.NetworkParams()
	addr, err := btcutil.DecodeAddress(address, net)
	if err != nil {
		return false, value, err
	}
	amount, err := client.client.GetReceivedByAddressMinConf(addr, 0)
	if err != nil {
		return false, value, err
	}
	utxos, err := client.GetUTXOs(ctx, address, 999999, 0)
	if err != nil {
		return false, value, err
	}
	var balance int64
	for _, utxo := range utxos {
		balance = balance + utxo.Amount
	}
	return int64(amount.ToUnit(btcutil.AmountSatoshi)) >= value && balance == 0, balance, nil
}

func (client *bitcoinFNClient) ScriptSpent(ctx context.Context, scriptAddress, spenderAddress string) (bool, string, error) {
	if err := client.client.ImportAddressRescan(scriptAddress, "", false); err != nil {
		return false, "", err
	}

	if err := client.client.ImportAddressRescan(spenderAddress, "", false); err != nil {
		return false, "", err
	}

	txs, err := client.client2.ListTransansactions()
	if err != nil {
		return false, "", err
	}

	var hash string
	for _, tx := range txs {
		if tx.Address == scriptAddress && tx.Category == "receive" {
			hash = tx.TxID
		}
	}

	txList, err := client.client2.ListReceivedByAddress(spenderAddress)
	if err != nil {
		return false, "", err
	}

	for _, txID := range txList[0].TxIDs {
		txHash, err := chainhash.NewHashFromStr(txID)
		if err != nil {
			return false, "", err
		}

		tx, err := client.client.GetRawTransaction(txHash)
		if err != nil {
			return false, "", err
		}

		for _, txIn := range tx.MsgTx().TxIn {
			if txIn.PreviousOutPoint.Hash.String() == hash {
				return true, hex.EncodeToString(txIn.SignatureScript), nil
			}
		}
	}

	return false, "", fmt.Errorf("could not find the transaction")
}

func (client *bitcoinFNClient) PublishTransaction(ctx context.Context, stx *wire.MsgTx) error {
	_, err := client.client.SendRawTransaction(stx, false)
	return err
}

func (client *bitcoinFNClient) NetworkParams() *chaincfg.Params {
	return client.params
}
