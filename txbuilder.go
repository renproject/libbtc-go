package libbtc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

type txBuilder struct {
	version   int32
	fee, dust int64
	client    Client
}

func NewTxBuilder(client Client) TxBuilder {
	return &txBuilder{2, 10000, 600, client}
}

type TxBuilder interface {
	Build(ctx context.Context, pubKey ecdsa.PublicKey, to string, contract []byte, value, mwIns, scriptIns int64) (Tx, error)
	BuildOmni(ctx context.Context, pubKey ecdsa.PublicKey, to string, contract []byte, token, tokenValue, btcValue, mwIns, scriptIns int64) (Tx, error)
}

type Tx interface {
	Hashes() [][]byte
	InjectSigs(sigs []*btcec.Signature) error
	Submit(ctx context.Context) ([]byte, error)
}

type transaction struct {
	sent      int64
	msgTx     *wire.MsgTx
	hashes    [][]byte
	client    Client
	contract  []byte
	publicKey ecdsa.PublicKey
	mwIns     int64
}

func (builder *txBuilder) Build(
	ctx context.Context,
	pubKey ecdsa.PublicKey,
	to string,
	contract []byte,
	value, mwIns, scriptIns int64,
) (Tx, error) {
	pubKeyBytes, err := builder.client.SerializePublicKey((*btcec.PublicKey)(&pubKey))
	if err != nil {
		return nil, err
	}

	from, err := builder.client.PublicKeyToAddress(pubKeyBytes)
	if err != nil {
		return nil, err
	}

	toAddr, err := btcutil.DecodeAddress(to, builder.client.NetworkParams())
	if err != nil {
		return nil, err
	}

	msgTx := wire.NewMsgTx(builder.version)

	var sent int64
	amt, pubKeyScript, err := fundBtcTx(ctx, from, nil, builder.client, msgTx, int(mwIns))
	if err != nil {
		return nil, err
	}
	if contract != nil {
		amt2, _, err := fundBtcTx(ctx, from, contract, builder.client, msgTx, int(scriptIns))
		if err != nil {
			return nil, err
		}
		amt += amt2
		sent = amt2 - builder.fee
	}

	if len(msgTx.TxIn) != int(mwIns+scriptIns) {
		return nil, fmt.Errorf("utxos spent")
	}

	fmt.Println("utxos being used: ")
	for i, txIn := range msgTx.TxIn {
		fmt.Printf("[%d]: %s\n", i, txIn.PreviousOutPoint.Hash.String())
	}

	if amt < value+builder.fee {
		return nil, fmt.Errorf("insufficient balance to do the transfer:"+
			"got: %d required: %d", amt, value+builder.fee)
	}

	if value > 0 {
		sent = value
		script, err := txscript.PayToAddrScript(toAddr)
		if err != nil {
			return nil, err
		}
		msgTx.AddTxOut(wire.NewTxOut(value, script))
	}

	if amt-value > builder.fee+builder.dust {
		P2PKHScript, err := txscript.PayToAddrScript(from)
		if err != nil {
			return nil, err
		}
		msgTx.AddTxOut(wire.NewTxOut(amt-value-builder.fee, P2PKHScript))
	}

	var hashes [][]byte

	for i := 0; i < int(mwIns); i++ {
		hash, err := txscript.CalcSignatureHash(pubKeyScript, txscript.SigHashAll, msgTx, i)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}

	for i := int(mwIns); i < int(scriptIns+mwIns); i++ {
		hash, err := txscript.CalcSignatureHash(contract, txscript.SigHashAll, msgTx, i)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}

	return &transaction{
		sent:      sent,
		hashes:    hashes,
		msgTx:     msgTx,
		client:    builder.client,
		publicKey: pubKey,
		contract:  contract,
		mwIns:     mwIns,
	}, nil
}

func (tx *transaction) Hashes() [][]byte {
	return tx.hashes
}

func (tx *transaction) InjectSigs(sigs []*btcec.Signature) error {
	pubKey := (*btcec.PublicKey)(&tx.publicKey)
	serializedPublicKey, err := tx.client.SerializePublicKey(pubKey)
	if err != nil {
		return err
	}
	for i, sig := range sigs {
		builder := txscript.NewScriptBuilder()
		builder.AddData(append(sig.Serialize(), byte(txscript.SigHashAll)))
		builder.AddData(serializedPublicKey)
		if int64(i) >= tx.mwIns && tx.contract != nil {
			builder.AddData(tx.contract)
		}
		sigScript, err := builder.Script()
		if err != nil {
			return err
		}
		tx.msgTx.TxIn[i].SignatureScript = sigScript
	}
	return nil
}

func (tx *transaction) Submit(ctx context.Context) ([]byte, error) {
	if err := tx.client.PublishTransaction(ctx, tx.msgTx); err != nil {
		return nil, err
	}
	return hex.DecodeString(tx.msgTx.TxHash().String())
}

func fundBtcTx(ctx context.Context, from btcutil.Address, script []byte, client Client, msgTx *wire.MsgTx, n int) (int64, []byte, error) {
	if script != nil {
		scriptAddr, err := btcutil.NewAddressScriptHash(script, client.NetworkParams())
		if err != nil {
			return 0, nil, err
		}
		from = scriptAddr
	}

	utxos, err := client.GetUTXOs(ctx, from.EncodeAddress(), int64(n), 0)
	if err != nil {
		return 0, nil, err
	}
	if len(utxos) < n {
		return 0, nil, fmt.Errorf("insufficient utxos requirex: %d got: %d", n, len(utxos))
	}

	var amount int64
	var scriptPubKey []byte
	for _, j := range utxos {
		ScriptPubKey, err := hex.DecodeString(j.ScriptPubKey)
		if err != nil {
			return 0, nil, err
		}
		if len(scriptPubKey) == 0 {
			scriptPubKey = ScriptPubKey
		} else {
			if bytes.Compare(scriptPubKey, ScriptPubKey) != 0 {
				continue
			}
		}

		hash, err := chainhash.NewHashFromStr(j.TxHash)
		if err != nil {
			return 0, nil, err
		}
		msgTx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(hash, j.Vout), []byte{}, [][]byte{}))
		amount += j.Amount
	}

	if script != nil {
		return amount, script, nil
	}
	return amount, scriptPubKey, nil
}
