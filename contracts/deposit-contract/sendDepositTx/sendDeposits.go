package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	contracts "github.com/prysmaticlabs/prysm/contracts/deposit-contract"
	prysmKeyStore "github.com/prysmaticlabs/prysm/shared/keystore"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/version"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
	rand2 "golang.org/x/exp/rand"
	"gonum.org/v1/gonum/stat/distuv"
)

var (
	log = logrus.WithField("prefix", "main")
)

func main() {
	var keystoreUTCPath string
	var prysmKeystorePath string
	var ipcPath string
	var passwordFile string
	var httpPath string
	var privKeyString string
	var depositContractAddr string
	var numberOfDeposits int64
	var depositAmount int64
	var depositDelay int64
	var variableTx bool
	var txDeviation int64
	var randomKey bool

	customFormatter := new(prefixed.TextFormatter)
	customFormatter.TimestampFormat = "2006-01-02 15:04:05"
	customFormatter.FullTimestamp = true
	logrus.SetFormatter(customFormatter)

	app := cli.NewApp()
	app.Name = "sendDepositTx"
	app.Usage = "this is a util to send deposit transactions"
	app.Version = version.GetVersion()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "keystoreUTCPath",
			Usage:       "Location of keystore",
			Destination: &keystoreUTCPath,
		},
		cli.StringFlag{
			Name:        "prysm-keystore",
			Usage:       "The path to the existing prysm keystore. This flag is ignored if used with --random-key",
			Destination: &prysmKeystorePath,
		},
		cli.StringFlag{
			Name:        "ipcPath",
			Usage:       "Filename for IPC socket/pipe within the datadir",
			Destination: &ipcPath,
		},
		cli.StringFlag{
			Name:        "httpPath",
			Value:       "http://localhost:8545/",
			Usage:       "HTTP-RPC server listening interface",
			Destination: &httpPath,
		},
		cli.StringFlag{
			Name:        "passwordFile",
			Value:       "./password.txt",
			Usage:       "Password file for unlock account",
			Destination: &passwordFile,
		},
		cli.StringFlag{
			Name:        "privKey",
			Usage:       "Private key to send ETH transaction",
			Destination: &privKeyString,
		},
		cli.StringFlag{
			Name:        "depositContract",
			Usage:       "Address of the deposit contract",
			Destination: &depositContractAddr,
		},
		cli.Int64Flag{
			Name:        "numberOfDeposits",
			Value:       1,
			Usage:       "number of deposits to send to the contract",
			Destination: &numberOfDeposits,
		},
		cli.Int64Flag{
			Name:        "depositAmount",
			Value:       3200,
			Usage:       "Maximum deposit value allowed in contract(in gwei)",
			Destination: &depositAmount,
		},
		cli.Int64Flag{
			Name:        "depositDelay",
			Value:       5,
			Usage:       "The time delay between sending the deposits to the contract(in seconds)",
			Destination: &depositDelay,
		},
		cli.BoolFlag{
			Name:        "variableTx",
			Usage:       "This enables variable transaction latencies to simulate real-world transactions",
			Destination: &variableTx,
		},
		cli.Int64Flag{
			Name:        "txDeviation",
			Usage:       "The standard deviation between transaction times",
			Value:       2,
			Destination: &txDeviation,
		},
		cli.BoolFlag{
			Name:        "random-key",
			Usage:       "Use a randomly generated keystore key",
			Destination: &randomKey,
		},
	}

	app.Action = func(c *cli.Context) {
		// Set up RPC client
		var rpcClient *rpc.Client
		var err error
		var txOps *bind.TransactOpts

		// Uses HTTP-RPC if IPC is not set
		if ipcPath == "" {
			rpcClient, err = rpc.Dial(httpPath)
		} else {
			rpcClient, err = rpc.Dial(ipcPath)
		}
		if err != nil {
			log.Fatal(err)
		}

		client := ethclient.NewClient(rpcClient)
		depositAmountInGwei := uint64(depositAmount)
		depositAmount = depositAmount * 1e9

		// User inputs private key, sign tx with private key
		if privKeyString != "" {
			privKey, err := crypto.HexToECDSA(privKeyString)
			if err != nil {
				log.Fatal(err)
			}
			txOps = bind.NewKeyedTransactor(privKey)
			txOps.Value = big.NewInt(depositAmount)
			txOps.GasLimit = 4000000
			// User inputs keystore json file, sign tx with keystore json
		} else {
			password := loadTextFromFile(passwordFile)

			// #nosec - Inclusion of file via variable is OK for this tool.
			keyJSON, err := ioutil.ReadFile(keystoreUTCPath)
			if err != nil {
				log.Fatal(err)
			}
			privKey, err := keystore.DecryptKey(keyJSON, password)
			if err != nil {
				log.Fatal(err)
			}

			txOps = bind.NewKeyedTransactor(privKey.PrivateKey)
			txOps.Value = big.NewInt(depositAmount)
			txOps.GasLimit = 4000000
		}

		depositContract, err := contracts.NewDepositContract(common.HexToAddress(depositContractAddr), client)
		if err != nil {
			log.Fatal(err)
		}

		statDist := buildStatisticalDist(depositDelay, numberOfDeposits, txDeviation)

		validatorKeys := make(map[string]*prysmKeyStore.Key)
		if randomKey {
			validatorKey, err := prysmKeyStore.NewKey(rand.Reader)
			validatorKeys[hex.EncodeToString(validatorKey.PublicKey.Marshal())] = validatorKey
			if err != nil {
				log.Errorf("Could not generate random key: %v", err)
			}
		} else {
			// Load from keystore
			store := prysmKeyStore.NewKeystore(prysmKeystorePath)
			rawPassword := loadTextFromFile(passwordFile)
			prefix := params.BeaconConfig().ValidatorPrivkeyFileName
			validatorKeys, err = store.GetKeys(prysmKeystorePath, prefix, rawPassword)
			if err != nil {
				log.WithField("path", prysmKeystorePath).WithField("password", rawPassword).Errorf("Could not get keys: %v", err)
			}
		}

		for _, validatorKey := range validatorKeys {
			data, err := prysmKeyStore.DepositInput(validatorKey, validatorKey, depositAmountInGwei)
			if err != nil {
				log.Errorf("Could not generate deposit input data: %v", err)
				continue
			}

			for i := int64(0); i < numberOfDeposits; i++ {
				//TODO(#2658): Use actual compressed pubkeys in G1 here
				tx, err := depositContract.Deposit(txOps, data.PublicKey, data.WithdrawalCredentials, data.Signature)
				if err != nil {
					log.Error("unable to send transaction to contract")
				}

				log.WithFields(logrus.Fields{
					"Transaction Hash": fmt.Sprintf("%#x", tx.Hash()),
				}).Infof("Deposit %d sent to contract address %v for validator with a public key %#x", i, depositContractAddr, validatorKey.PublicKey.Marshal())

				// If flag is enabled make transaction times variable
				if variableTx {
					time.Sleep(time.Duration(math.Abs(statDist.Rand())) * time.Second)
					continue
				}

				time.Sleep(time.Duration(depositDelay) * time.Second)
			}
		}
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func buildStatisticalDist(depositDelay int64, numberOfDeposits int64, txDeviation int64) *distuv.StudentsT {
	src := rand2.NewSource(uint64(time.Now().Unix()))
	dist := &distuv.StudentsT{
		Mu:    float64(depositDelay),
		Sigma: float64(txDeviation),
		Nu:    float64(numberOfDeposits - 1),
		Src:   src,
	}

	return dist
}

func loadTextFromFile(filepath string) string {
	// #nosec - Inclusion of file via variable is OK for this tool.
	file, err := os.Open(filepath)
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanWords)
	scanner.Scan()
	return scanner.Text()
}
