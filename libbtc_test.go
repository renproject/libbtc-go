package libbtc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/renproject/libbtc-go"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/hdkeychain"
	"github.com/tyler-smith/go-bip39"
)

var _ = Describe("LibBTC", func() {
	loadMasterKey := func(network uint32) (*hdkeychain.ExtendedKey, error) {
		switch network {
		case 1:
			seed := bip39.NewSeed(os.Getenv("TESTNET_MNEMONIC"), os.Getenv("TESTNET_PASSPHRASE"))
			return hdkeychain.NewMaster(seed, &chaincfg.TestNet3Params)
		case 0:
			seed := bip39.NewSeed(os.Getenv("MNEMONIC"), os.Getenv("PASSPHRASE"))
			return hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
		default:
			return nil, NewErrUnsupportedNetwork(fmt.Sprintf("network id: %d", network))
		}
	}

	loadKey := func(path ...uint32) (*ecdsa.PrivateKey, error) {
		key, err := loadMasterKey(path[1])
		if err != nil {
			return nil, err
		}
		for _, val := range path {
			key, err = key.Child(val)
			if err != nil {
				return nil, err
			}
		}
		privKey, err := key.ECPrivKey()
		if err != nil {
			return nil, err
		}
		return privKey.ToECDSA(), nil
	}

	buildClients := func() []Client {
		APIClient, err := NewMercuryClient("testnet")
		if err != nil {
			panic(err)
		}
		// FNClient, err := NewBitcoinFNClient(os.Getenv("RPC_URL"), os.Getenv("RPC_USER"), os.Getenv("RPC_PASSWORD"))
		// if err != nil {
		// 	panic(err)
		// }
		return []Client{APIClient /*, FNClient*/}
	}

	getAccounts := func(client Client) (Account, Account) {
		mainKey, err := loadKey(44, 1, 0, 0, 0) // "m/44'/1'/0'/0/0"
		if err != nil {
			panic(err)
		}
		mainAccount := NewAccount(client, mainKey, nil)
		secKey, err := loadKey(44, 1, 1, 0, 0) // "m/44'/1'/1'/0/0"
		if err != nil {
			panic(err)
		}
		secondaryAccount := NewAccount(client, secKey, nil)
		return mainAccount, secondaryAccount
	}

	for _, client := range buildClients() {
		var secret [32]byte
		rand.Read(secret[:])

		Context("when interacting with testnet", func() {
			It("should get a valid address of an account", func() {
				mainAccount, _ := getAccounts(client)
				addr, err := mainAccount.Address()
				Expect(err).Should(BeNil())
				Expect(addr.IsForNet(&chaincfg.TestNet3Params)).Should(BeTrue())
				fmt.Println("Address: ", addr)
			})

			It("should get correct network of an account", func() {
				mainAccount, _ := getAccounts(client)
				Expect(mainAccount.NetworkParams()).Should(Equal(&chaincfg.TestNet3Params))
			})

			It("should get a valid serialized public key of an account", func() {
				mainAccount, _ := getAccounts(client)
				pubKey, err := mainAccount.SerializedPublicKey()
				Expect(err).Should(BeNil())
				Expect(btcec.IsCompressedPubKey(pubKey)).Should(BeFalse())
				_, err = btcec.ParsePubKey(pubKey, btcec.S256())
				Expect(err).Should(BeNil())
			})

			It("should create a valid deterministic slave address", func() {
				mainAccount, _ := getAccounts(client)
				pubKey, err := mainAccount.SerializedPublicKey()
				Expect(err).Should(BeNil())
				nonce := [20]byte{}
				rand.Read(nonce[:])
				slaveAddr1, err := mainAccount.SlaveAddress(btcutil.Hash160(pubKey), nonce[:])
				slaveAddr2, err := mainAccount.SlaveAddress(btcutil.Hash160(pubKey), nonce[:])
				Expect(reflect.DeepEqual(slaveAddr1, slaveAddr2)).Should(BeTrue())
			})

			It("should get the balance of an address", func() {
				mainAccount, _ := getAccounts(client)
				addr, err := mainAccount.Address()
				Expect(err).Should(BeNil())
				balance, err := mainAccount.Balance(context.Background(), addr.String(), 0)
				Expect(err).Should(BeNil())
				fmt.Printf("%s: %d SAT", addr, balance)
			})

			FIt("should get a utxo", func() {
				mainAccount, _ := getAccounts(client)
				addr, err := mainAccount.Address()
				Expect(err).Should(BeNil())
				utxos, err := mainAccount.GetUTXOs(context.Background(), addr.EncodeAddress(), 1, 0)
				Expect(err).Should(BeNil())
				actualUTXO := utxos[0]
				utxo, err := mainAccount.GetUTXO(context.Background(), actualUTXO.TxHash, actualUTXO.Vout)
				Expect(err).Should(BeNil())
				fmt.Println(actualUTXO, utxo)
				Expect(reflect.DeepEqual(actualUTXO, utxo)).Should(BeTrue())
			})

			It("should transfer 10000 SAT to another address", func() {
				mainAccount, secondaryAccount := getAccounts(client)
				secAddr, err := secondaryAccount.Address()
				Expect(err).Should(BeNil())
				initialBalance, err := secondaryAccount.Balance(context.Background(), secAddr.String(), 0)
				Expect(err).Should(BeNil())
				// building a transaction to transfer bitcoin to the secondary address
				_, _, err = mainAccount.Transfer(context.Background(), secAddr.String(), 10000, Fast, false)
				Expect(err).Should(BeNil())
				finalBalance, err := secondaryAccount.Balance(context.Background(), secAddr.String(), 0)
				Expect(err).Should(BeNil())
				Expect(finalBalance - initialBalance).Should(Equal(int64(10000)))
			})

			It("should transfer 10000 SAT to another address", func() {
				mainKey, err := loadKey(44, 1, 0, 0, 0) // "m/44'/1'/0'/0/0"
				Expect(err).Should(BeNil())
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				mainPrivKey := (*btcec.PrivateKey)(mainKey)

				mainAccount, secondaryAccount := getAccounts(client)
				mainAddr, err := mainAccount.Address()
				Expect(err).Should(BeNil())
				secAddr, err := secondaryAccount.Address()
				Expect(err).Should(BeNil())
				utxos, err := client.GetUTXOs(ctx, mainAddr.String(), 1000, 0)
				Expect(err).Should(BeNil())
				builder := NewTxBuilder(client)
				tx, err := builder.Build(ctx, mainKey.PublicKey, secAddr.String(), nil, 20000, utxos, nil)
				Expect(err).Should(BeNil())

				hashes := tx.Hashes()
				sigs := make([]*btcec.Signature, len(hashes))
				for i, hash := range hashes {
					sigs[i], err = mainPrivKey.Sign(hash)
					Expect(err).Should(BeNil())
				}
				Expect(tx.InjectSigs(sigs)).Should(BeNil())

				initialBalance, err := secondaryAccount.Balance(context.Background(), secAddr.String(), 0)
				Expect(err).Should(BeNil())
				// building a transaction to transfer bitcoin to the secondary address
				txHash, err := tx.Submit(ctx)
				Expect(err).Should(BeNil())

				fmt.Printf(mainAccount.FormatTransactionView("successfully submitted transfer tx", hex.EncodeToString(txHash)))
				finalBalance, err := secondaryAccount.Balance(context.Background(), secAddr.String(), 0)
				Expect(err).Should(BeNil())
				Expect(finalBalance - initialBalance).Should(Equal(int64(10000)))
			})

			It("should transfer 10000 SAT from a slave address", func() {
				mainKey, err := loadKey(44, 1, 0, 0, 0) // "m/44'/1'/0'/0/0"
				Expect(err).Should(BeNil())
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				mainPrivKey := (*btcec.PrivateKey)(mainKey)

				mainAccount, secondaryAccount := getAccounts(client)
				nonce := [32]byte{}
				rand.Read(nonce[:])
				pubKeyBytes, err := client.SerializePublicKey((*btcec.PublicKey)(&mainPrivKey.PublicKey))
				Expect(err).Should(BeNil())
				slaveAddr, err := mainAccount.SlaveAddress(btcutil.Hash160(pubKeyBytes), nonce[:])
				Expect(err).Should(BeNil())
				slaveScript, err := mainAccount.SlaveScript(btcutil.Hash160(pubKeyBytes), nonce[:])
				Expect(err).Should(BeNil())
				_, _, err = mainAccount.Transfer(ctx, slaveAddr.String(), 30000, Fast, false)
				Expect(err).Should(BeNil())
				mainAddr, err := mainAccount.Address()
				Expect(err).Should(BeNil())
				scriptUtxos, err := client.GetUTXOs(ctx, slaveAddr.String(), 1000, 0)
				Expect(err).Should(BeNil())

				utxos, err := client.GetUTXOs(ctx, mainAddr.String(), 1000, 0)
				Expect(err).Should(BeNil())
				builder := NewTxBuilder(client)
				tx, err := builder.Build(ctx, mainKey.PublicKey, mainAddr.String(), slaveScript, 20000, utxos, scriptUtxos)
				Expect(err).Should(BeNil())

				hashes := tx.Hashes()
				sigs := make([]*btcec.Signature, len(hashes))
				for i, hash := range hashes {
					sigs[i], err = mainPrivKey.Sign(hash)
					Expect(err).Should(BeNil())
				}
				Expect(tx.InjectSigs(sigs)).Should(BeNil())

				initialBalance, err := secondaryAccount.Balance(context.Background(), mainAddr.String(), 0)
				Expect(err).Should(BeNil())
				// building a transaction to transfer bitcoin to the secondary address
				txHash, err := tx.Submit(ctx)
				Expect(err).Should(BeNil())
				fmt.Printf(mainAccount.FormatTransactionView("successfully submitted transfer tx", hex.EncodeToString(txHash)))
				finalBalance, err := secondaryAccount.Balance(context.Background(), mainAddr.String(), 0)
				Expect(err).Should(BeNil())
				Expect(finalBalance - initialBalance).Should(Equal(int64(20000)))
			})
		})
	}
})
