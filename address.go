package libbtc

import (
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil"
)

func (client *client) SlaveAddress(mpkh, nonce []byte) (btcutil.Address, error) {
	b := txscript.NewScriptBuilder()
	b.AddData(nonce)
	b.AddOp(txscript.OP_DROP)
	b.AddOp(txscript.OP_DUP)
	b.AddOp(txscript.OP_HASH160)
	b.AddData(mpkh)
	b.AddOp(txscript.OP_EQUALVERIFY)
	b.AddOp(txscript.OP_CHECKSIG)
	script, err := b.Script()
	if err != nil {
		return nil, nil
	}
	return btcutil.NewAddressScriptHash(script, client.NetworkParams())
}
