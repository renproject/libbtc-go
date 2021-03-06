package libbtc

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const BitcoinDust = 600
const MaxBitcoinFee = int64(10000)

type tx struct {
	receiveValues   []int64
	scriptPublicKey []byte
	account         *account
	msgTx           *wire.MsgTx
	ctx             context.Context
}

func (account *account) newTx(ctx context.Context, msgtx *wire.MsgTx) *tx {
	return &tx{
		msgTx:   msgtx,
		account: account,
		ctx:     ctx,
	}
}

func (tx *tx) fund(addr btcutil.Address) error {
	if addr == nil {
		var err error
		addr, err = tx.account.Address()
		if err != nil {
			return err
		}
	}

	var value int64
	for i, j := range tx.msgTx.TxOut {
		if j.Value < 600 {
			return fmt.Errorf("transaction's %d output value (%d) is less than bitcoin's minimum value (%d)", i, j.Value, BitcoinDust)
		}
		value = value + j.Value
	}

	balance, err := tx.account.Balance(tx.ctx, addr.EncodeAddress(), 0)
	if err != nil {
		return err
	}

	if value+MaxBitcoinFee > balance {
		return NewErrInsufficientBalance(addr.EncodeAddress(), value+MaxBitcoinFee, balance)
	}

	utxos, err := tx.account.GetUTXOs(tx.ctx, addr.EncodeAddress(), 999999, 0)
	if err != nil {
		return err
	}

	for _, j := range utxos {
		ScriptPubKey, err := hex.DecodeString(j.ScriptPubKey)
		if err != nil {
			return err
		}
		if len(tx.scriptPublicKey) == 0 {
			tx.scriptPublicKey = ScriptPubKey
		} else {
			if bytes.Compare(tx.scriptPublicKey, ScriptPubKey) != 0 {
				continue
			}
		}
		tx.receiveValues = append(tx.receiveValues, j.Amount)
		hash, err := chainhash.NewHashFromStr(j.TxHash)
		if err != nil {
			return err
		}
		tx.msgTx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(hash, j.Vout), []byte{}, [][]byte{}))
		value = value - j.Amount
		if value <= -MaxBitcoinFee {
			break
		}
	}

	if value <= -MaxBitcoinFee {
		P2PKHScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			return err
		}
		tx.msgTx.AddTxOut(wire.NewTxOut(-value, P2PKHScript))
	} else {
		return ErrMismatchedPubKeys
	}
	return nil
}

func (tx *tx) fundAll(addr btcutil.Address) error {
	utxos, err := tx.account.GetUTXOs(tx.ctx, addr.EncodeAddress(), 1000, 0)
	if err != nil {
		return err
	}
	for _, j := range utxos {
		ScriptPubKey, err := hex.DecodeString(j.ScriptPubKey)
		if err != nil {
			return err
		}
		if len(tx.scriptPublicKey) == 0 {
			tx.scriptPublicKey = ScriptPubKey
		} else {
			if bytes.Compare(tx.scriptPublicKey, ScriptPubKey) != 0 {
				continue
			}
		}
		tx.receiveValues = append(tx.receiveValues, j.Amount)
		hash, err := chainhash.NewHashFromStr(j.TxHash)
		if err != nil {
			return err
		}
		tx.msgTx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(hash, j.Vout), []byte{}, [][]byte{}))
	}
	return nil
}

func (tx *tx) sign(f func(*txscript.ScriptBuilder), updateTxIn func(*wire.TxIn), contract []byte) error {
	var subScript []byte
	if contract == nil {
		subScript = tx.scriptPublicKey
	} else {
		subScript = contract
	}
	serializedPublicKey, err := tx.account.SerializedPublicKey()
	if err != nil {
		return err
	}
	for i, txin := range tx.msgTx.TxIn {
		if updateTxIn != nil {
			updateTxIn(txin)
		}
		sig, err := txscript.RawTxInSignature(tx.msgTx, i, subScript, txscript.SigHashAll, tx.account.PrivKey)
		if err != nil {
			return err
		}
		builder := txscript.NewScriptBuilder()
		builder.AddData(sig)
		builder.AddData(serializedPublicKey)
		if f != nil {
			f(builder)
		}
		if contract != nil {
			builder.AddData(contract)
		}
		sigScript, err := builder.Script()
		if err != nil {
			return err
		}
		txin.SignatureScript = sigScript
	}
	return nil
}

func (tx *tx) estimateSTXSize(f func(*txscript.ScriptBuilder), updateTxIn func(*wire.TxIn), contract []byte) (int, error) {
	var subScript []byte
	if contract == nil {
		subScript = tx.scriptPublicKey
	} else {
		subScript = contract
	}
	serializedPublicKey, err := tx.account.SerializedPublicKey()
	if err != nil {
		return 0, err
	}
	txCopy := tx.msgTx.Copy()
	for i, txin := range txCopy.TxIn {
		if updateTxIn != nil {
			updateTxIn(txin)
		}
		sig, err := txscript.RawTxInSignature(txCopy, i, subScript, txscript.SigHashAll, tx.account.PrivKey)
		if err != nil {
			return 0, err
		}
		builder := txscript.NewScriptBuilder()
		builder.AddData(sig)
		builder.AddData(serializedPublicKey)
		if f != nil {
			f(builder)
		}
		if contract != nil {
			builder.AddData(contract)
		}
		sigScript, err := builder.Script()
		if err != nil {
			return 0, err
		}
		txin.SignatureScript = sigScript
	}
	return txCopy.SerializeSize(), nil
}

func (tx *tx) verify() error {
	for i, receiveValue := range tx.receiveValues {
		engine, err := txscript.NewEngine(tx.scriptPublicKey, tx.msgTx, i,
			txscript.StandardVerifyFlags, txscript.NewSigCache(10),
			txscript.NewTxSigHashes(tx.msgTx), receiveValue)
		if err != nil {
			return err
		}
		if err := engine.Execute(); err != nil {
			return err
		}
	}
	return nil
}

func (tx *tx) submit() error {
	return tx.account.PublishTransaction(tx.ctx, tx.msgTx)
}
