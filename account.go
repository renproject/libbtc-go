package libbtc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/sirupsen/logrus"
)

// The TxExecutionSpeed indicates the tier of speed that the transaction falls
// under while writing to the blockchain.
type TxExecutionSpeed uint8

// TxExecutionSpeed values.
const (
	Nil = TxExecutionSpeed(iota)
	Slow
	Standard
	Fast
)

type account struct {
	PrivKey *btcec.PrivateKey
	Logger  logrus.FieldLogger
	Client
}

// Account is an Bitcoin external account that can sign and submit transactions
// to the Bitcoin blockchain. An Account is an abstraction over the Bitcoin
// blockchain.
type Account interface {
	Client
	BTCClient() Client
	Address() (btcutil.Address, error)
	SerializedPublicKey() ([]byte, error)
	Transfer(ctx context.Context, to string, value int64, speed TxExecutionSpeed, sendAll bool) (string, int64, error)
	BuildTransfer(ctx context.Context, to string, value int64, speed TxExecutionSpeed, sendAll bool) (string, []byte, error)
	SendTransaction(
		ctx context.Context,
		script []byte,
		speed TxExecutionSpeed,
		updateTxIn func(*wire.TxIn),
		preCond func(*wire.MsgTx) bool,
		f func(*txscript.ScriptBuilder),
		postCond func(*wire.MsgTx) bool,
		sendAll bool,
	) (string, int64, error)
	BuildTransaction(
		ctx context.Context,
		contract []byte,
		speed TxExecutionSpeed,
		updateTxIn func(*wire.TxIn),
		preCond func(*wire.MsgTx) bool,
		f func(*txscript.ScriptBuilder),
		postCond func(*wire.MsgTx) bool,
		sendAll bool,
	) (string, []byte, error)
}

// NewAccount returns a user account for the provided private key which is
// connected to a Bitcoin client.
func NewAccount(client Client, privateKey *ecdsa.PrivateKey, logger logrus.FieldLogger) Account {
	if logger == nil {
		nullLogger := logrus.New()
		logFile, err := os.OpenFile(os.DevNull, os.O_APPEND|os.O_WRONLY, 0666)
		if err != nil {
			panic(err)
		}
		nullLogger.SetOutput(logFile)
		logger = nullLogger
	}
	return &account{
		(*btcec.PrivateKey)(privateKey),
		logger,
		client,
	}
}

// Address returns the address of the given private key
func (account *account) Address() (btcutil.Address, error) {
	pubKeyBytes, err := account.SerializedPublicKey()
	if err != nil {
		return nil, err
	}
	return account.PublicKeyToAddress(pubKeyBytes)
}

// Transfer bitcoins to the given address
func (account *account) Transfer(ctx context.Context, to string, value int64, speed TxExecutionSpeed, sendAll bool) (string, int64, error) {
	if sendAll {
		me, err := account.Address()
		if err != nil {
			return "", 0, err
		}
		balance, err := account.Balance(ctx, me.EncodeAddress(), 0)
		if err != nil {
			return "", 0, err
		}
		value = balance
	}

	address, err := btcutil.DecodeAddress(to, account.NetworkParams())
	if err != nil {
		return "", 0, err
	}
	return account.SendTransaction(
		ctx,
		nil,
		speed,
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
		nil,
		sendAll,
	)
}

// BuildTransfer bitcoins to the given address
func (account *account) BuildTransfer(ctx context.Context, to string, value int64, speed TxExecutionSpeed, sendAll bool) (string, []byte, error) {
	if sendAll {
		me, err := account.Address()
		if err != nil {
			return "", nil, err
		}
		balance, err := account.Balance(ctx, me.EncodeAddress(), 0)
		if err != nil {
			return "", nil, err
		}
		value = balance
	}

	address, err := btcutil.DecodeAddress(to, account.NetworkParams())
	if err != nil {
		return "", nil, err
	}
	return account.BuildTransaction(
		ctx,
		nil,
		speed,
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
		nil,
		sendAll,
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
	speed TxExecutionSpeed,
	updateTxIn func(*wire.TxIn),
	preCond func(*wire.MsgTx) bool,
	f func(*txscript.ScriptBuilder),
	postCond func(*wire.MsgTx) bool,
	sendAll bool,
) (string, int64, error) {
	// Current Bitcoin Transaction Version (2).
	tx := account.newTx(ctx, wire.NewMsgTx(2))
	if preCond != nil && !preCond(tx.msgTx) {
		return "", 0, ErrPreConditionCheckFailed
	}

	var address btcutil.Address
	var err error
	if contract == nil {
		address, err = account.Address()
		if err != nil {
			return "", 0, err
		}
	} else {
		address, err = btcutil.NewAddressScriptHash(contract, account.NetworkParams())
		if err != nil {
			return "", 0, err
		}
	}

	account.Logger.Infof("funding %s, with fee %d SAT/byte", address.EncodeAddress(), speed)
	if sendAll {
		if err := tx.fundAll(address); err != nil {
			return "", 0, err
		}
	} else {
		if err := tx.fund(address); err != nil {
			return "", 0, err
		}
	}
	account.Logger.Info("successfully funded the transaction")

	account.Logger.Info("estimating stx size")
	size, err := tx.estimateSTXSize(f, updateTxIn, contract)
	if err != nil {
		return "", 0, err
	}
	account.Logger.Info("successfully estimated stx size")

	rate, err := SuggestedTxRate(speed)
	if err != nil {
		rate = 30
	}

	txFee := int64(size) * rate
	if txFee > MaxBitcoinFee-BitcoinDust {
		txFee = MaxBitcoinFee
	}
	tx.msgTx.TxOut[len(tx.msgTx.TxOut)-1].Value -= txFee

	account.Logger.Info("signing the tx")
	if err := tx.sign(f, updateTxIn, contract); err != nil {
		return "", 0, err
	}
	account.Logger.Info("successfully signined the tx")

	account.Logger.Info("verifying the tx")
	if err := tx.verify(); err != nil {
		return "", 0, err
	}
	account.Logger.Info("successfully verified the tx")

	for {
		account.Logger.Info("trying to submit the tx")
		select {
		case <-ctx.Done():
			account.Logger.Info("submitting failed due to failed post condition")
			return "", 0, ErrPostConditionCheckFailed
		default:
			if err := tx.submit(); err != nil {
				account.Logger.Infof("submitting failed due to %s", err)
				return "", 0, err
			}
			for i := 0; i < 60; i++ {
				if postCond == nil || postCond(tx.msgTx) {
					account.Logger.Infof("successfully submitted the tx", err)
					return tx.msgTx.TxHash().String(), txFee, nil
				}
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func (account *account) BuildTransaction(
	ctx context.Context,
	contract []byte,
	speed TxExecutionSpeed,
	updateTxIn func(*wire.TxIn),
	preCond func(*wire.MsgTx) bool,
	f func(*txscript.ScriptBuilder),
	postCond func(*wire.MsgTx) bool,
	sendAll bool,
) (string, []byte, error) {
	// Current Bitcoin Transaction Version (2).
	tx := account.newTx(ctx, wire.NewMsgTx(2))
	if preCond != nil && !preCond(tx.msgTx) {
		return "", nil, ErrPreConditionCheckFailed
	}

	var address btcutil.Address
	var err error
	if contract == nil {
		address, err = account.Address()
		if err != nil {
			return "", nil, err
		}
	} else {
		address, err = btcutil.NewAddressScriptHash(contract, account.NetworkParams())
		if err != nil {
			return "", nil, err
		}
	}

	account.Logger.Infof("funding %s, with fee %d SAT/byte", address.EncodeAddress(), speed)
	if sendAll {
		if err := tx.fundAll(address); err != nil {
			return "", nil, err
		}
	} else {
		if err := tx.fund(address); err != nil {
			return "", nil, err
		}
	}
	account.Logger.Info("successfully funded the transaction")

	account.Logger.Info("estimating stx size")
	size, err := tx.estimateSTXSize(f, updateTxIn, contract)
	if err != nil {
		return "", nil, err
	}
	account.Logger.Info("successfully estimated stx size")

	rate, err := SuggestedTxRate(speed)
	if err != nil {
		rate = 30
	}

	txFee := int64(size) * rate
	if txFee > MaxBitcoinFee-BitcoinDust {
		txFee = MaxBitcoinFee
	}
	tx.msgTx.TxOut[len(tx.msgTx.TxOut)-1].Value -= txFee

	account.Logger.Info("signing the tx")
	if err := tx.sign(f, updateTxIn, contract); err != nil {
		return "", nil, err
	}
	account.Logger.Info("successfully signined the tx")

	account.Logger.Info("verifying the tx")
	if err := tx.verify(); err != nil {
		return "", nil, err
	}
	account.Logger.Info("successfully verified the tx")

	var stxBuffer bytes.Buffer
	stxBuffer.Grow(tx.msgTx.SerializeSize())
	if err := tx.msgTx.Serialize(&stxBuffer); err != nil {
		return "", nil, err
	}

	return tx.msgTx.TxHash().String(), stxBuffer.Bytes(), nil
}

func (account *account) SerializedPublicKey() ([]byte, error) {
	return account.SerializePublicKey(account.PrivKey.PubKey())
}

func (account *account) BTCClient() Client {
	return account.Client
}

// SuggestedTxRate returns the gas price that bitcoinfees.earn.com recommends for
// transactions to be mined on Bitcoin blockchain based on the speed provided.
func SuggestedTxRate(txSpeed TxExecutionSpeed) (int64, error) {
	request, err := http.NewRequest("GET", "https://bitcoinfees.earn.com/api/v1/fees/recommended", nil)
	if err != nil {
		return 0, fmt.Errorf("cannot build request to bitcoinfees.earn.com = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	res, err := (&http.Client{}).Do(request)
	if err != nil {
		return 0, fmt.Errorf("cannot connect to bitcoinfees.earn.com = %v", err)
	}
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code %v from bitcoinfees.earn.com", res.StatusCode)
	}

	data := struct {
		Slow     int64 `json:"fastestFee"`
		Standard int64 `json:"halfHourFee"`
		Fast     int64 `json:"hourFee"`
	}{}
	if err = json.NewDecoder(res.Body).Decode(&data); err != nil {
		resp, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("cannot decode response body (%s) from bitcoinfees.earn.com = %v", resp, err)
	}

	switch txSpeed {
	case Slow:
		return data.Slow, nil
	case Standard:
		return data.Standard, nil
	case Fast:
		return data.Fast, nil
	default:
		return 0, fmt.Errorf("invalid speed tier: %v", txSpeed)
	}
}
