package libbtc

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

func (builder *txBuilder) BuildOmni(
	ctx context.Context,
	pubKey ecdsa.PublicKey,
	to string,
	contract []byte,
	token, tokenValue,
	btcValue, mwIns, scriptIns int64,
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
		fmt.Printf("[%d]: %s:%d\n", i, txIn.PreviousOutPoint.Hash.String(), txIn.PreviousOutPoint.Index)
	}

	if amt < btcValue+builder.fee+546 {
		return nil, fmt.Errorf("insufficient balance to do the transfer:"+
			"got: %d required: %d", amt, btcValue+builder.fee+546)
	}

	if tokenValue > 0 {
		script, err := txscript.PayToAddrScript(toAddr)
		if err != nil {
			return nil, err
		}
		msgTx.AddTxOut(wire.NewTxOut(546, script))
		omniScript, err := buildOmniScript(token, tokenValue)
		if err != nil {
			return nil, err
		}
		msgTx.AddTxOut(wire.NewTxOut(0, omniScript))
	}

	if amt > builder.fee+builder.dust+546 {
		P2PKHScript, err := txscript.PayToAddrScript(from)
		if err != nil {
			return nil, err
		}
		msgTx.AddTxOut(wire.NewTxOut(amt-builder.fee-546, P2PKHScript))
	}

	var hashes [][]byte

	for i := 0; i < int(mwIns); i++ {
		hash, err := txscript.CalcSignatureHash(pubKeyScript, txscript.SigHashAll, msgTx, 0)
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

func buildOmniScript(token, amount int64) ([]byte, error) {
	data, err := hex.DecodeString(fmt.Sprintf("6f6d6e6900000000%08x%016x", token, amount))
	if err != nil {
		return nil, err
	}
	b := txscript.NewScriptBuilder()
	b.AddOp(txscript.OP_RETURN)
	b.AddData(data)
	return b.Script()
}
