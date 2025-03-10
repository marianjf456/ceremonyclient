package app

import (
	"encoding/binary"

	"go.uber.org/zap"
	"source.quilibrium.com/quilibrium/monorepo/node/keys"
	"source.quilibrium.com/quilibrium/monorepo/node/p2p"
	"source.quilibrium.com/quilibrium/monorepo/node/store"
)

type Node struct {
	logger          *zap.Logger
	dataProofStore  store.DataProofStore
	clockStore      store.ClockStore
	coinStore       store.CoinStore
	hypergraphStore store.HypergraphStore
	keyManager      keys.KeyManager
	pebble          store.KVDB
}

type DHTNode struct {
	pubSub p2p.PubSub
	quit   chan struct{}
}

func newDHTNode(
	pubSub p2p.PubSub,
) (*DHTNode, error) {
	return &DHTNode{
		pubSub: pubSub,
		quit:   make(chan struct{}),
	}, nil
}

func newNode(
	logger *zap.Logger,
	dataProofStore store.DataProofStore,
	clockStore store.ClockStore,
	coinStore store.CoinStore,
	hypergraphStore store.HypergraphStore,
	keyManager keys.KeyManager,
	pebble store.KVDB,
) (*Node, error) {

	return &Node{
		logger,
		dataProofStore,
		clockStore,
		coinStore,
		hypergraphStore,
		keyManager,
		pebble,
	}, nil
}

func GetOutputs(output []byte) (
	index uint32,
	indexProof []byte,
	kzgCommitment []byte,
	kzgProof []byte,
) {
	index = binary.BigEndian.Uint32(output[:4])
	indexProof = output[4:520]
	kzgCommitment = output[520:594]
	kzgProof = output[594:668]
	return index, indexProof, kzgCommitment, kzgProof
}

func nearestApplicablePowerOfTwo(number uint64) uint64 {
	power := uint64(128)
	if number > 2048 {
		power = 65536
	} else if number > 1024 {
		power = 2048
	} else if number > 128 {
		power = 1024
	}
	return power
}

func (d *DHTNode) Start() {
	<-d.quit
}

func (d *DHTNode) Stop() {
	go func() {
		d.quit <- struct{}{}
	}()
}

func (n *Node) Start() {
}

func (n *Node) Stop() {
	n.pebble.Close()
}

func (n *Node) GetLogger() *zap.Logger {
	return n.logger
}

func (n *Node) GetClockStore() store.ClockStore {
	return n.clockStore
}

func (n *Node) GetCoinStore() store.CoinStore {
	return n.coinStore
}

func (n *Node) GetDataProofStore() store.DataProofStore {
	return n.dataProofStore
}

func (n *Node) GetHypergraphStore() store.HypergraphStore {
	return n.hypergraphStore
}

func (n *Node) GetKeyManager() keys.KeyManager {
	return n.keyManager
}
