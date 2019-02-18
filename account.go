package libbtc

import (
	"context"
	"crypto/ecdsa"
	"time"

	"github.com/btcsuite/btcd/chaincfg"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

type account struct {
	PrivKey *btcec.PrivateKey
	Client
}

// Account is an Bitcoin external account that can sign and submit transactions
// to the Bitcoin blockchain. An Account is an abstraction over the Bitcoin
// blockchain.
type Account interface {
	Client
	Address() (btcutil.Address, error)
	SerializedPublicKey() ([]byte, error)
	Transfer(ctx context.Context, to string, value, fee int64) (string, error)
	SendTransaction(
		ctx context.Context,
		script []byte,
		fee int64,
		updateTxIn func(*wire.TxIn),
		preCond func(*wire.MsgTx) bool,
		f func(*txscript.ScriptBuilder),
		postCond func(*wire.MsgTx) bool,
	) error
}

// NewAccount returns a user account for the provided private key which is
// connected to a Bitcoin client.
func NewAccount(client Client, privateKey *ecdsa.PrivateKey) Account {
	return &account{
		(*btcec.PrivateKey)(privateKey),
		client,
	}
}

// Address returns the address of the given private key
func (account *account) Address() (btcutil.Address, error) {
	pubKeyBytes, err := account.SerializedPublicKey()
	if err != nil {
		return nil, err
	}
	pubKey, err := btcutil.NewAddressPubKey(pubKeyBytes, account.NetworkParams())
	if err != nil {
		return nil, err
	}
	addrString := pubKey.EncodeAddress()
	return btcutil.DecodeAddress(addrString, account.NetworkParams())
}

// Transfer bitcoins to the given address
func (account *account) Transfer(ctx context.Context, to string, value, fee int64) (string, error) {
	address, err := btcutil.DecodeAddress(to, account.NetworkParams())
	if err != nil {
		return "", err
	}
	var txHash string
	return txHash, account.SendTransaction(
		ctx,
		nil,
		fee,
		nil,
		func(tx *wire.MsgTx) bool {
			P2PKHScript, err := txscript.PayToAddrScript(address)
			if err != nil {
				return false
			}
			tx.AddTxOut(wire.NewTxOut(value, P2PKHScript))
			return true
		},
		nil,
		func(tx *wire.MsgTx) bool {
			txHash = tx.TxHash().String()
			return true
		},
	)
}

// SendTransaction builds, signs, verifies and publishes a transaction to the
// corresponding blockchain. If contract is provided then the transaction uses
// the contract's unspent outputs for the transaction, otherwise uses the
// account's unspent outputs to fund the transaction. preCond is executed in
// the starting of the process, if it returns false SendTransaction returns
// ErrPreConditionCheckFailed and stops the process. This function can be used
// to modify how the unspent outputs are spent, this can be nil. f is supposed
// to be used with non empty contracts, to modify the signature script. preCond
// is executed in the starting of the process, if it returns false
// SendTransaction returns ErrPreConditionCheckFailed and stops the process.
func (account *account) SendTransaction(
	ctx context.Context,
	contract []byte,
	fee int64,
	updateTxIn func(*wire.TxIn),
	preCond func(*wire.MsgTx) bool,
	f func(*txscript.ScriptBuilder),
	postCond func(*wire.MsgTx) bool,
) error {
	// Current Bitcoin Transaction Version (2).
	tx := account.newTx(ctx, wire.NewMsgTx(2))
	if preCond != nil && !preCond(tx.msgTx) {
		return ErrPreConditionCheckFailed
	}

	var address btcutil.Address
	var err error
	if contract == nil {
		address, err = account.Address()
		if err != nil {
			return err
		}
	} else {
		address, err = btcutil.NewAddressScriptHash(contract, account.NetworkParams())
		if err != nil {
			return err
		}
	}

	if err := tx.fund(address, fee); err != nil {
		return err
	}

	if err := tx.sign(f, updateTxIn, contract); err != nil {
		return err
	}

	if err := tx.verify(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ErrPostConditionCheckFailed
		default:
			if err := tx.submit(); err != nil {
				return err
			}
			for i := 0; i < 60; i++ {
				if postCond == nil || postCond(tx.msgTx) {
					return nil
				}
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func (account *account) SerializedPublicKey() ([]byte, error) {
	pubKey := account.PrivKey.PubKey()
	switch account.NetworkParams() {
	case &chaincfg.MainNetParams:
		return pubKey.SerializeCompressed(), nil
	case &chaincfg.TestNet3Params:
		return pubKey.SerializeUncompressed(), nil
	default:
		return nil, NewErrUnsupportedNetwork(account.NetworkParams().Name)
	}
}
