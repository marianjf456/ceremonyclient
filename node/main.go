//go:build !js && !wasm

package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"net/http"
	npprof "net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	rdebug "runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cloudflare/circl/sign/ed448"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pbnjay/memory"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/crypto/sha3"
	"google.golang.org/protobuf/proto"
	"source.quilibrium.com/quilibrium/monorepo/node/app"
	"source.quilibrium.com/quilibrium/monorepo/node/config"
	qcrypto "source.quilibrium.com/quilibrium/monorepo/node/crypto"
	"source.quilibrium.com/quilibrium/monorepo/node/crypto/kzg"
	qruntime "source.quilibrium.com/quilibrium/monorepo/node/internal/runtime"
	"source.quilibrium.com/quilibrium/monorepo/node/protobufs"
	"source.quilibrium.com/quilibrium/monorepo/node/rpc"
	"source.quilibrium.com/quilibrium/monorepo/node/store"
	"source.quilibrium.com/quilibrium/monorepo/node/utils"
)

var (
	configDirectory = flag.String(
		"config",
		filepath.Join(".", ".config"),
		"the configuration directory",
	)
	balance = flag.Bool(
		"balance",
		false,
		"print the node's confirmed token balance to stdout and exit",
	)
	dbConsole = flag.Bool(
		"db-console",
		false,
		"starts the node in database console mode",
	)
	importPrivKey = flag.String(
		"import-priv-key",
		"",
		"creates a new config using a specific key from the phase one ceremony",
	)
	peerId = flag.Bool(
		"peer-id",
		false,
		"print the peer id to stdout from the config and exit",
	)
	cpuprofile = flag.String(
		"cpuprofile",
		"",
		"write cpu profile to file",
	)
	memprofile = flag.String(
		"memprofile",
		"",
		"write memory profile after 20m to this file",
	)
	pprofServer = flag.String(
		"pprof-server",
		"",
		"enable pprof server on specified address (e.g. localhost:6060)",
	)
	prometheusServer = flag.String(
		"prometheus-server",
		"",
		"enable prometheus server on specified address (e.g. localhost:8080)",
	)
	nodeInfo = flag.Bool(
		"node-info",
		false,
		"print node related information",
	)
	debug = flag.Bool(
		"debug",
		false,
		"sets log output to debug (verbose)",
	)
	dhtOnly = flag.Bool(
		"dht-only",
		false,
		"sets a node to run strictly as a dht bootstrap peer (not full node)",
	)
	network = flag.Uint(
		"network",
		0,
		"sets the active network for the node (mainnet = 0, primary testnet = 1)",
	)
	signatureCheck = flag.Bool(
		"signature-check",
		signatureCheckDefault(),
		"enables or disables signature validation (default true or value of QUILIBRIUM_SIGNATURE_CHECK env var)",
	)
	core = flag.Int(
		"core",
		0,
		"specifies the core of the process (defaults to zero, the initial launcher)",
	)
	parentProcess = flag.Int(
		"parent-process",
		0,
		"specifies the parent process pid for a data worker",
	)
	integrityCheck = flag.Bool(
		"integrity-check",
		false,
		"runs an integrity check on the store, helpful for confirming backups are not corrupted (defaults to false)",
	)
	lightProver = flag.Bool(
		"light-prover",
		true,
		"when enabled, frame execution validation is skipped",
	)
	compactDB = flag.Bool(
		"compact-db",
		false,
		"compacts the database and exits",
	)
)

func signatureCheckDefault() bool {
	envVarValue, envVarExists := os.LookupEnv("QUILIBRIUM_SIGNATURE_CHECK")
	if envVarExists {
		def, err := strconv.ParseBool(envVarValue)
		if err == nil {
			return def
		} else {
			fmt.Println("Invalid environment variable QUILIBRIUM_SIGNATURE_CHECK, must be 'true' or 'false'. Got: " + envVarValue)
		}
	}

	return true
}

func main() {
	flag.Parse()

	if *signatureCheck {
		if runtime.GOOS == "windows" {
			fmt.Println("Signature check not available for windows yet, skipping...")
		} else {
			ex, err := os.Executable()
			if err != nil {
				panic(err)
			}

			b, err := os.ReadFile(ex)
			if err != nil {
				fmt.Println(
					"Error encountered during signature check – are you running this " +
						"from source? (use --signature-check=false)",
				)
				panic(err)
			}

			checksum := sha3.Sum256(b)
			digest, err := os.ReadFile(ex + ".dgst")
			if err != nil {
				fmt.Println("Digest file not found")
				os.Exit(1)
			}

			parts := strings.Split(string(digest), " ")
			if len(parts) != 2 {
				fmt.Println("Invalid digest file format")
				os.Exit(1)
			}

			digestBytes, err := hex.DecodeString(parts[1][:64])
			if err != nil {
				fmt.Println("Invalid digest file format")
				os.Exit(1)
			}

			if !bytes.Equal(checksum[:], digestBytes) {
				fmt.Println("Invalid digest for node")
				os.Exit(1)
			}

			count := 0

			for i := 1; i <= len(config.Signatories); i++ {
				signatureFile := fmt.Sprintf(ex+".dgst.sig.%d", i)
				sig, err := os.ReadFile(signatureFile)
				if err != nil {
					continue
				}

				pubkey, _ := hex.DecodeString(config.Signatories[i-1])
				if !ed448.Verify(pubkey, digest, sig, "") {
					fmt.Printf("Failed signature check for signatory #%d\n", i)
					os.Exit(1)
				}
				count++
			}

			if count < ((len(config.Signatories)-4)/2)+((len(config.Signatories)-4)%2) {
				fmt.Printf("Quorum on signatures not met")
				os.Exit(1)
			}

			fmt.Println("Signature check passed")
		}
	} else {
		fmt.Println("Signature check disabled, skipping...")
	}

	if *memprofile != "" && *core == 0 {
		go func() {
			for {
				time.Sleep(5 * time.Minute)
				f, err := os.Create(*memprofile)
				if err != nil {
					log.Fatal(err)
				}
				pprof.WriteHeapProfile(f)
				f.Close()
			}
		}()
	}

	if *cpuprofile != "" && *core == 0 {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *pprofServer != "" && *core == 0 {
		go func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", npprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", npprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", npprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", npprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", npprof.Trace)
			log.Fatal(http.ListenAndServe(*pprofServer, mux))
		}()
	}

	if *prometheusServer != "" && *core == 0 {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			log.Fatal(http.ListenAndServe(*prometheusServer, mux))
		}()
	}

	if *balance {
		config, err := config.LoadConfig(*configDirectory, "", false)
		if err != nil {
			panic(err)
		}

		printBalance(config)

		return
	}

	if *peerId {
		config, err := config.LoadConfig(*configDirectory, "", false)
		if err != nil {
			panic(err)
		}

		printPeerID(config.P2P)
		return
	}

	if *importPrivKey != "" {
		config, err := config.LoadConfig(*configDirectory, *importPrivKey, false)
		if err != nil {
			panic(err)
		}

		printPeerID(config.P2P)
		fmt.Println("Import completed, you are ready for the launch.")
		return
	}

	if *nodeInfo {
		config, err := config.LoadConfig(*configDirectory, "", false)
		if err != nil {
			panic(err)
		}

		printNodeInfo(config)
		return
	}

	if !*dbConsole && *core == 0 {
		config.PrintLogo()
		config.PrintVersion(uint8(*network))
		fmt.Println(" ")
	}

	nodeConfig, err := config.LoadConfig(*configDirectory, "", false)
	if err != nil {
		panic(err)
	}

	if *compactDB && *core == 0 {
		db := store.NewPebbleDB(nodeConfig.DB)
		if err := db.CompactAll(); err != nil {
			panic(err)
		}
		if err := db.Close(); err != nil {
			panic(err)
		}
		return
	}

	if *network != 0 {
		if nodeConfig.P2P.BootstrapPeers[0] == config.BootstrapPeers[0] {
			fmt.Println(
				"Node has specified to run outside of mainnet but is still " +
					"using default bootstrap list. This will fail. Exiting.",
			)
			os.Exit(1)
		}

		nodeConfig.Engine.GenesisSeed = fmt.Sprintf(
			"%02x%s",
			byte(*network),
			nodeConfig.Engine.GenesisSeed,
		)
		nodeConfig.P2P.Network = uint8(*network)
		fmt.Println(
			"Node is operating outside of mainnet – be sure you intended to do this.",
		)
	}

	// If it's not explicitly set to true, we should defer to flags
	if !nodeConfig.Engine.FullProver {
		nodeConfig.Engine.FullProver = !*lightProver
	}

	clearIfTestData(*configDirectory, nodeConfig)

	if *dbConsole {
		console, err := app.NewDBConsole(nodeConfig)
		if err != nil {
			panic(err)
		}

		console.Run()
		return
	}

	if *dhtOnly {
		done := make(chan os.Signal, 1)
		signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
		dht, err := app.NewDHTNode(nodeConfig)
		if err != nil {
			panic(err)
		}

		go func() {
			dht.Start()
		}()

		<-done
		dht.Stop()
		return
	}

	if len(nodeConfig.Engine.DataWorkerMultiaddrs) == 0 {
		maxProcs, numCPU := runtime.GOMAXPROCS(0), runtime.NumCPU()
		if maxProcs > numCPU && !nodeConfig.Engine.AllowExcessiveGOMAXPROCS {
			fmt.Println("GOMAXPROCS is set higher than the number of available CPUs.")
			os.Exit(1)
		}

		nodeConfig.Engine.DataWorkerCount = qruntime.WorkerCount(
			nodeConfig.Engine.DataWorkerCount, true,
		)
	}

	if *core != 0 {
		rdebug.SetMemoryLimit(nodeConfig.Engine.DataWorkerMemoryLimit)

		if *parentProcess == 0 && len(nodeConfig.Engine.DataWorkerMultiaddrs) == 0 {
			panic("parent process pid not specified")
		}

		l, err := zap.NewProduction()
		if err != nil {
			panic(err)
		}

		rpcMultiaddr := fmt.Sprintf(
			nodeConfig.Engine.DataWorkerBaseListenMultiaddr,
			int(nodeConfig.Engine.DataWorkerBaseListenPort)+*core-1,
		)

		if len(nodeConfig.Engine.DataWorkerMultiaddrs) != 0 {
			rpcMultiaddr = nodeConfig.Engine.DataWorkerMultiaddrs[*core-1]
		}

		srv, err := rpc.NewDataWorkerIPCServer(
			rpcMultiaddr,
			l,
			uint32(*core)-1,
			qcrypto.NewWesolowskiFrameProver(l),
			nodeConfig,
			*parentProcess,
		)
		if err != nil {
			panic(err)
		}

		err = srv.Start()
		if err != nil {
			panic(err)
		}
		return
	} else {
		totalMemory := int64(memory.TotalMemory())
		dataWorkerReservedMemory := int64(0)
		if len(nodeConfig.Engine.DataWorkerMultiaddrs) == 0 {
			dataWorkerReservedMemory = nodeConfig.Engine.DataWorkerMemoryLimit * int64(nodeConfig.Engine.DataWorkerCount)
		}
		switch availableOverhead := totalMemory - dataWorkerReservedMemory; {
		case totalMemory < dataWorkerReservedMemory:
			fmt.Println("The memory allocated to data workers exceeds the total system memory.")
			fmt.Println("You are at risk of running out of memory during runtime.")
		case availableOverhead < 8*1024*1024*1024:
			fmt.Println("The memory available to the node, unallocated to the data workers, is less than 8GiB.")
			fmt.Println("You are at risk of running out of memory during runtime.")
		default:
			if _, explicitGOMEMLIMIT := os.LookupEnv("GOMEMLIMIT"); !explicitGOMEMLIMIT {
				rdebug.SetMemoryLimit(availableOverhead * 8 / 10)
			}
			if _, explicitGOGC := os.LookupEnv("GOGC"); !explicitGOGC {
				rdebug.SetGCPercent(10)
			}
		}
	}

	fmt.Println("Loading ceremony state and starting node...")

	if !*integrityCheck {
		go spawnDataWorkers(nodeConfig)
		defer stopDataWorkers()
	}

	kzg.Init()

	report := RunSelfTestIfNeeded(*configDirectory, nodeConfig)

	if *core == 0 {
		for {
			genesis, err := config.DownloadAndVerifyGenesis(uint(nodeConfig.P2P.Network))
			if err != nil {
				time.Sleep(10 * time.Minute)
				continue
			}

			nodeConfig.Engine.GenesisSeed = genesis.GenesisSeedHex
			break
		}
	}

	// done := make(chan os.Signal, 1)
	// signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	var node *app.Node
	if *debug {
		node, err = app.NewDebugNode(nodeConfig, report)
	} else {
		node, err = app.NewNode(nodeConfig, report)
	}

	if err != nil {
		panic(err)
	}

	vertexIds := []string{
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d904000067d676550f8bb82a0afeb8c8f4dfa2c596ed6be0b9633055bb82b4b3de",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d904000074bac4cb3cd3661006a270979b61db138c607645a195de4743fac41b34",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d90400008267c0b3e1db177177504cad6e224e9402ce520b0f4999ff8fd52d08f3",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d90400008333485f75647f41c6c6204d49944ce5883f6f984e8215e9c593f4d518",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d904000092b20991086f350cc26943ecdf1a5d792e781d888d1870f5cf2ed87d80",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9040000cc5cb85dc360d1df8b6aeaf88e9c9301d61a3ed771ca855de6b505c5ca",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9040000d2f05dde06258245959d30a366fccdcc83463fb3e7634cf7099948c5f3",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9040000df5c93e1defbaaef7bbf87a3c056f00558dff00989441509c5a0315647",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9040000e7927010847e5fc31fb13745ae5d130e9b9d535a87bf8152402821785b",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9040000ea8458585ba7f52e5b3366b49a27c7e3c56ffcb218f65a836358e520fd",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9040000fa9cfe741f7b4d7fc35e7193c3406bc44c65fa99ed77b2469217e218db",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d90400010c0719130a2f118f72d6621d138c566b252a77594beecc486db6a10d37",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d904000118e9dcfc17d16e3bba318663de47b1904e33c437effe29f3b8c4e30535",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d90400011bc9169b2e90b6e2f744eec52a8d5117d2559335978d16939fed6db12b",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001b56e3460be4fa992f2aa75f7eb3216b3ec9196476c270d60e047a3986a",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001b705ddb3b09a597dd3b98f81460bf94f0889226d251193522c647f3cd7",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001bc686550b0c2bca0373a7ea726a2e7d5f8f4f413b4a92456e60578d836",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001cc1cb61fa45d58c8e00ea2eb8edffee4b4510b402427d3f387dc21f080",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001cec3fc58b0972030cd880db26a7de87f7b2e1e0b17de26faed59fd22f6",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001dd0c5b30ccc0c8d0efab39e7e13dbadeb2a1d21ba6035c3cc8a6fbfd91",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001e43497d2c05f4984e57641d9de47030f664767344553edc632a9b5e108",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001ec7d523904d18ae1de67d15f6862d4477765c471ac6e643076e312602a",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001f093a752a1e0e9bb63634acf0486749594df9f1adace3eca5e2e730656",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9080001f4ada6ded1030743105553cd73c8f86ecd745e4638be710f206f878ac1",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d90800020bb0445a55bb62ddf590ec32ee254d08ed2bcfce991f2ecca416d084da",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d910433356e0f8202d9a276aaf47e7e659a5fa3ad541443fcc633061afd05ea62c",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d910471fc1216096bc79bf8d54f24f81a4b401593b7a019124f2aebd0ba9a8c790",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d910495b6b0bd4b1cd287c1c9d905d8415ef6a40e6779c5029fe2fe915ede4ad84",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d910495b80ab5484edc1b6e922761fec15c71a379d2ada66c37f1a18d2a1be4062",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9104bff1614ac7a9f59586dfe0b6f12e709f35d36f1160ae19d6774597ffbd261",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9104fdbc4e5f2b5e4d9d6341a4c5dfc742d79e3c99346f51215bb404885bc3b8a",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012732795f23f028cedf2af727b8e9bec0f099f062a734050bfa1e33aa16e",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d918001275b0cf3107fcd99b7e45e54e5d893c4ad223fcf41cab667e5b142a77e2",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d918001283cc52010c985c4a90546baa3e57bca878ba6807f2c91a5333b235ab08",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012859e9f6397e2268da291d61b2e88996d36c8a84a2dd4527867a006035f",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d918001288593b1f341aedd833fc7663eb369178895c41dfd5df1613c1a2e75087",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012a7c7c7169686b37c8fcc603f9e788bded64a37664e2993803f87cff085",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012aa6ccb550559248b6888d55568ff82b45735d4fae7bf321a22cbd74375",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012bc7928e2dab7d3158e7ebfe0f451bbd6d3ccdb54745831928b8a78f00a",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012cb50d4226d8532c5510977d41202ec2483c34d084228375d88710235f1",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012d3e832dec5fdfc3c656d43504a12b16a6421e3e3b70822aadb5ac5d8bd",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9180012e7a4068bf80da1f7a6c37f6c54caa920d2b86f13ebfc85766e4b0e33a3",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d918001307a7c9312816c4c7ff891da37e5cf3c433d683d26b86b948142d3acd47",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d918001388697c0b89c1acc49626fd56ecc43ede6b09820f5e98546e3dc1a00881",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b063898ebea9f2976bfbcbb52c0fc4ea1b13d85eb70c2482033e296a2230",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b0644ee33ee673460724e3b9cced7db347caf5919d8427a8223b466b3cad",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b0809e71a4d79ef8fc4bd4b0333691d290a12f2436693a14a5ccd3fcffda",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b081f7c1c37409e20aed95d7299cbbbf3cd0b14bd712638bf9f8b5b66830",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b082a43ee9525dd92c2e15f0afbdf9cd4c125a16cb420d9227062d0f67ab",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b0995a7aad2e7b8e36cb6d15e4036e9939945139da983b14ce37b98117de",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b09fd7b1695a9ed1454698049ada395f2ff08d9f6b509c72ae33e2780a5b",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b0a760ee4f8d254e98b91821c35019309b790b12c14261af94671880f932",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92400b0ace684e9e1fb74a3f5c989959a34db83975a43a0f7c823ced5228416af",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9270000503d3bd15d6c44ede68df75f156f0c4a49b96dc9f26c87d2c8c2aa0d83",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9270000519561693a308bc43e20340a412d600c38ade8bf73667896d69ba8e7ac",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d927000054415101126cc00c84db445225a7fdd8a4f993d869d984d5dbd8ac1c4d",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9270000544c9a47174d61a0c05d8b43eadf8674f1d3d624929201b052e30c89be",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d927000058e741f5e8a171dd32546ca60039eab810bcfdf83ec8ac9bacf888716a",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92700006076cd2603d6e44511d12e203a95f58f34c68269dca90d877c907c2970",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d927000068a125026b0ba1bc857f98668a4259c67be12697a79a77ae37181ae0e6",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92700006a93130bc7aaa4ccaa84d304eb71957728a1f77dc133028b06c2855c37",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9270000713dfd3a4ac943a6937a4eec408359429dd37321b869e277648b075c78",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d927000074ad3f7d45094b345fb9977c16c1466f62da6e85e8631c0cdd74cabadc",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d927000074df5341e4a928fb97f7739f3d74bf2b9bafb3c775bc0e9b1efa3552cb",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92700007898b415d164f5851f9509bebd200f012c3d51d298086b63dcf6f7f092",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d927000081a1963a568bddf7c09b1deb315ede626922f65cd574c54f0cf324708e",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92700008f619837880836b546bab8dfbf5be2d52defaa87f2247838ec379d8d24",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92800005b666e592e488304ec283a67c1789a3e34afba787f177d9501ac646ec6",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92800005d57e4b10d3824f637fb4cee1b1f855aac915015222c4652e6429dca69",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d928000079c51ad3c2edfb53238a5a0195cc8f9d2471fe0ee6a74c8ff5304e13d4",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92800007ead0d90a6bba22d47945d2cb58300609b330ac81c7f003c8d8680d0e5",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92800008c065f38c08a8bff1658043b75d451b121100339e9b18641f1958e9d70",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9280000924f1271a5b85e32ff25415a15f06e71db14d080fb5a0f679888962250",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9280000927fd64b1c10254005ab8fb83daba6db16658d407ffbd9ad2a78b361ae",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9280000952df8ac82460ab5374fb6a199b3a15134f7333090062f9a1ee9e0ac87",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9280000960b2b3dd12ccf91739717137372c3bb2f5ca1890400b1d088c3f5dca4",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92800009dfa6e9badccee5f72bc294c3c5da0e9f6435617a0265227af55ac0bd9",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9280000a2a60ebb77e0ec54aa5916cb0b4a57e2f8458969c44e71309e62748250",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9280000b29db0615e5b0d214be0a17f1a8db662290d73425515e5d68b1fb2034b",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92900007cba90c824446a24da5f5e3588f27eb28110c335ae927b892b9d71aafc",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d929000081fb91a6ba38e5ab8aafef0a9d88c1c279d4e6219275fcad16f3453803",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d929000082078b37b476e525d1019303222809943627bd0eca92d05e68025e9b04",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d929000090ea069966e9a420b848f979d94a975811a915061704ee6c82e2821afe",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92900009c526b10517ed7a72a1dbe90b64c783e2a69ee24849bb4297118c921a2",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9290000ba50d76aeb604930a4b232395602d9ab1d97e231ab9ef4fdb9a3133e93",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9290000be2007eed7ec62281ec9b0cc0e5d1100dc89d236b4a1f8e84111790d05",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9290000be3192660c00d0001a416e5659ce5e45309fd48da47ed431ed1f801e75",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9290000c223a117920f9270776fcdd1d3f9ea8a2b6a7ba94798ec8c8d2692e18e",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d9290000d0fd19cd40725d8cea0eefeb32d7df84beb64f1e5fa100212039e5dd81",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a00003820b2e656959a6717ac69ab0df2a5af260a44e1843c885e349108912f",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a00007160f8622935989236557d40e1b537a6b0b811d9fe4d47df574b8cd306",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a000091018aefea3ac649a5eddc1c269c7e389b628ec7afa886b6adbca932ba",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a00009ab0b94deea30aa96f48356305b2fcd762b7572cc5e2e4fdbe49ca5573",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a00009c231ee634883ff752756aa329d2b296cfbd0a64f208f32469f883587e",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a0000ac9c6aa5ecfb250c86cfd50ef3486fe6a4be5b3c68e4d6df1eb4f2dcdb",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a0000ae37b40359f9c658b26cddf4fb507166a07c4ecb40732250d91bd84afd",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a0000bd8d44b40d21447e52f3f86f659e0af6099d6fd7e28ef48244441f3c2a",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a0000d29279babdd707fbeda855d4120c8a05b1d73a810ad3b44a549feeccd4",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a0000d4e23a5006891d780a07c62511e2b80ed6ff6040387381189a3d46e1fc",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92a0000df080b88375185206f4b8af3357b84ba8e74e55741dccfd085c6c32b5c",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffad24a3cc7294e838adfa3e389b0f07f8b1fc786ebb00a7b8f6b16f38441",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffae3439350c4c4e62bfeec22cb204165e89cab72ccd37c1997b9d40ce777",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffae524274d9f0d13407af24367f41bb24964817ef200b10db774b7947e27",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffaec11d1a3a05cdc1fc51f2c33a3f8c63e94e63405942a181c5d3af18af5",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffaf4b05281de31b35771e9f10ec7218c12618ee39ea7f0fef91d037b70f1",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb0005d64536c3cae6bc8d1a71309b9fd112e394e3e5e208e6396032fc19",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb0566b4e93c5a8fe1a14aa1df037cc4268d621503a61cfd87e1287bd350",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb07c688268d00a7103eba81ad7262e9259bdac14916e0b4bfb310765237",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb0a6eb895be0b4585a6b173d5f470005adda9ebb64283ae85ec09959aa3",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb1be577ab05b57539c9c17fd438f8ca200137d50469f626d7f1def880cc",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb20df0d6700e3a813256dde5f2899add38032b523998647eecf57c09ebf",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb21b8c36e791c530aa24c1df2049c98d99e34b3b5aeccb1462806626d54",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb2bbf15def0ab3e23f7a305046525fd62faade944ebe16990f2e69408a1",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb3665f69ab669e8e79a6e558e51700ed212e6fbeb43ed0be755f18ead2f",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb3e27cea6088a99f9c3320e57bb9967501da7307b02c7e21113bbdb48cb",
		"11558584af7017a9bfd1ff1864302d643fbe58c62dcf90cbcd8fde74a26794d92bfffb425ee94cde5c6687e3a4cb8efb18805997e75b4369f728856780ead2c3",
	}
	for _, id := range vertexIds {
		key, err := hex.DecodeString(id)
		if err != nil {
			panic(err)
		}
		raw, err := node.GetHypergraphStore().LoadRawVertexTree(key)
		if err != nil {
			fmt.Printf("%x\n", key)
			fmt.Println(err)
			fmt.Println()
		} else {
			fmt.Printf("%x\n%x\n\n", key, raw)
		}
	}

	node.Stop()

	// if *integrityCheck {
	// 	fmt.Println("Running integrity check...")
	// 	node.VerifyProofIntegrity()
	// 	fmt.Println("Integrity check passed!")
	// 	return
	// }

	// // runtime.GOMAXPROCS(1)

	// node.Start()
	// defer node.Stop()

	// if nodeConfig.ListenGRPCMultiaddr != "" {
	// 	srv, err := rpc.NewRPCServer(
	// 		nodeConfig.ListenGRPCMultiaddr,
	// 		nodeConfig.ListenRestMultiaddr,
	// 		node.GetLogger(),
	// 		node.GetDataProofStore(),
	// 		node.GetClockStore(),
	// 		node.GetCoinStore(),
	// 		node.GetKeyManager(),
	// 		node.GetPubSub(),
	// 		node.GetMasterClock(),
	// 		node.GetExecutionEngines(),
	// 	)
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	if err := srv.Start(); err != nil {
	// 		panic(err)
	// 	}
	// 	defer srv.Stop()
	// }

	// <-done
}

var dataWorkers []*exec.Cmd

func spawnDataWorkers(nodeConfig *config.Config) {
	if len(nodeConfig.Engine.DataWorkerMultiaddrs) != 0 {
		fmt.Println(
			"Data workers configured by multiaddr, be sure these are running...",
		)
		return
	}

	process, err := os.Executable()
	if err != nil {
		panic(err)
	}

	dataWorkers = make([]*exec.Cmd, nodeConfig.Engine.DataWorkerCount)
	fmt.Printf("Spawning %d data workers...\n", nodeConfig.Engine.DataWorkerCount)

	for i := 1; i <= nodeConfig.Engine.DataWorkerCount; i++ {
		i := i
		go func() {
			for {
				args := []string{
					fmt.Sprintf("--core=%d", i),
					fmt.Sprintf("--parent-process=%d", os.Getpid()),
				}
				args = append(args, os.Args[1:]...)
				cmd := exec.Command(process, args...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stdout
				err := cmd.Start()
				if err != nil {
					panic(err)
				}

				dataWorkers[i-1] = cmd
				cmd.Wait()
				time.Sleep(25 * time.Millisecond)
				fmt.Printf("Data worker %d stopped, restarting...\n", i)
			}
		}()
	}
}

func stopDataWorkers() {
	for i := 0; i < len(dataWorkers); i++ {
		err := dataWorkers[i].Process.Signal(os.Kill)
		if err != nil {
			fmt.Printf(
				"fatal: unable to kill worker with pid %d, please kill this process!\n",
				dataWorkers[i].Process.Pid,
			)
		}
	}
}

func RunSelfTestIfNeeded(
	configDir string,
	nodeConfig *config.Config,
) *protobufs.SelfTestReport {
	logger, _ := zap.NewProduction()

	cores := runtime.GOMAXPROCS(0)
	if len(nodeConfig.Engine.DataWorkerMultiaddrs) != 0 {
		cores = len(nodeConfig.Engine.DataWorkerMultiaddrs) + 1
	}

	memory := memory.TotalMemory()
	d, err := os.Stat(filepath.Join(configDir, "store"))
	if d == nil {
		err := os.Mkdir(filepath.Join(configDir, "store"), 0755)
		if err != nil {
			panic(err)
		}
	}

	report := &protobufs.SelfTestReport{}

	report.Cores = uint32(cores)
	report.Memory = binary.BigEndian.AppendUint64([]byte{}, memory)
	disk := utils.GetDiskSpace(nodeConfig.DB.Path)
	report.Storage = binary.BigEndian.AppendUint64([]byte{}, disk)
	logger.Info("writing report")

	report.Capabilities = []*protobufs.Capability{
		{
			ProtocolIdentifier: 0x020000,
		},
	}
	reportBytes, err := proto.Marshal(report)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(
		filepath.Join(configDir, "SELF_TEST"),
		reportBytes,
		fs.FileMode(0600),
	)
	if err != nil {
		panic(err)
	}

	return report
}

func clearIfTestData(configDir string, nodeConfig *config.Config) {
	_, err := os.Stat(filepath.Join(configDir, "RELEASE_VERSION"))
	if os.IsNotExist(err) {
		fmt.Println("Clearing test data...")
		err := os.RemoveAll(nodeConfig.DB.Path)
		if err != nil {
			panic(err)
		}

		versionFile, err := os.OpenFile(
			filepath.Join(configDir, "RELEASE_VERSION"),
			os.O_CREATE|os.O_RDWR,
			fs.FileMode(0600),
		)
		if err != nil {
			panic(err)
		}

		_, err = versionFile.Write([]byte{0x01, 0x00, 0x00})
		if err != nil {
			panic(err)
		}

		err = versionFile.Close()
		if err != nil {
			panic(err)
		}
	}
}

func printBalance(config *config.Config) {
	if config.ListenGRPCMultiaddr == "" {
		_, _ = fmt.Fprintf(os.Stderr, "gRPC Not Enabled, Please Configure\n")
		os.Exit(1)
	}

	conn, err := app.ConnectToNode(config)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := protobufs.NewNodeServiceClient(conn)

	balance, err := app.FetchTokenBalance(client)
	if err != nil {
		panic(err)
	}

	conversionFactor, _ := new(big.Int).SetString("1DCD65000", 16)
	r := new(big.Rat).SetFrac(balance.Owned, conversionFactor)
	fmt.Println("Owned balance:", r.FloatString(12), "QUIL")
	fmt.Println("Note: bridged balance is not reflected here, you must bridge back to QUIL to use QUIL on mainnet.")
}

func getPeerID(p2pConfig *config.P2PConfig) peer.ID {
	peerPrivKey, err := hex.DecodeString(p2pConfig.PeerPrivKey)
	if err != nil {
		panic(errors.Wrap(err, "error unmarshaling peerkey"))
	}

	privKey, err := crypto.UnmarshalEd448PrivateKey(peerPrivKey)
	if err != nil {
		panic(errors.Wrap(err, "error unmarshaling peerkey"))
	}

	pub := privKey.GetPublic()
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		panic(errors.Wrap(err, "error getting peer id"))
	}

	return id
}

func printPeerID(p2pConfig *config.P2PConfig) {
	id := getPeerID(p2pConfig)

	fmt.Println("Peer ID: " + id.String())
}

func printNodeInfo(cfg *config.Config) {
	if cfg.ListenGRPCMultiaddr == "" {
		_, _ = fmt.Fprintf(os.Stderr, "gRPC Not Enabled, Please Configure\n")
		os.Exit(1)
	}

	printPeerID(cfg.P2P)

	conn, err := app.ConnectToNode(cfg)
	if err != nil {
		fmt.Println("Could not connect to node. If it is still booting, please wait.")
		os.Exit(1)
	}
	defer conn.Close()

	client := protobufs.NewNodeServiceClient(conn)

	nodeInfo, err := app.FetchNodeInfo(client)
	if err != nil {
		panic(err)
	}

	fmt.Println("Version: " + config.FormatVersion(nodeInfo.Version))
	fmt.Println("Max Frame: " + strconv.FormatUint(nodeInfo.GetMaxFrame(), 10))
	if nodeInfo.ProverRing == -1 {
		fmt.Println("Not in Prover Ring")
	} else {
		fmt.Println("Prover Ring: " + strconv.FormatUint(
			uint64(nodeInfo.ProverRing),
			10,
		))
	}
	fmt.Println("Seniority: " + new(big.Int).SetBytes(
		nodeInfo.PeerSeniority,
	).String())
	fmt.Println("Active Workers:", nodeInfo.Workers)
	printBalance(cfg)
}
