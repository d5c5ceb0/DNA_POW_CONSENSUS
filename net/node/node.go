package node

import (
	. "DNA_POW/common"
	"DNA_POW/common/config"
	. "DNA_POW/common/config"
	"DNA_POW/common/log"
	"DNA_POW/core/ledger"
	"DNA_POW/core/transaction"
	"DNA_POW/crypto"
	"DNA_POW/events"
	. "DNA_POW/net/message"
	. "DNA_POW/net/protocol"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Semaphore chan struct{}

func MakeSemaphore(n int) Semaphore {
	return make(chan struct{}, n)
}

func (s Semaphore) acquire() { s <- struct{}{} }
func (s Semaphore) release() { <-s }

type node struct {
	//sync.RWMutex	//The Lock not be used as expected to use function channel instead of lock
	state     uint32   // node state
	id        uint64   // The nodes's id
	cap       [32]byte // The node capability set
	version   uint32   // The network protocol the node used
	services  uint64   // The services the node supplied
	relay     bool     // The relay capability of the node (merge into capbility flag)
	height    uint64   // The node latest block height
	txnCnt    uint64   // The transactions be transmit by this node
	rxTxnCnt  uint64   // The transaction received by this node
	publicKey *crypto.PubKey
	// TODO does this channel should be a buffer channel
	chF        chan func() error // Channel used to operate the node without lock
	link                         // The link status and infomation
	local      *node             // The pointer to local node
	nbrNodes                     // The neighbor node connect with currently node except itself
	eventQueue                   // The event queue to notice notice other modules
	TXNPool                      // Unconfirmed transaction pool
	idCache                      // The buffer to store the id of the items which already be processed
	/*
	 * |--|--|--|--|--|--|isSyncFailed|isSyncHeaders|
	 */
	syncFlag                 uint8
	flagLock                 sync.RWMutex
	flightHeights            []uint32
	cachelock                sync.RWMutex
	flightlock               sync.RWMutex
	invHashLock              sync.RWMutex
	requestedBlockLock       sync.RWMutex
	lastContact              time.Time
	nodeDisconnectSubscriber events.Subscriber
	tryTimes                 uint32
	cachedHashes             []Uint256
	ConnectingNodes
	RetryConnAddrs
	KnownAddressList
	MaxOutboundCnt     uint
	DefaultMaxPeers    uint
	GetAddrMax         uint
	TxNotifyChan       chan int
	headerFirstMode    bool
	invRequestHashes   []Uint256
	RequestedBlockList map[Uint256]time.Time
	// Checkpoints ordered from oldest to newest.
	NextCheckpoint *Checkpoint
	IsStartSync    bool
	SyncBlkReqSem  Semaphore
	SyncHdrReqSem  Semaphore
	StartHash      Uint256
	StopHash       Uint256
}

type RetryConnAddrs struct {
	sync.RWMutex
	RetryAddrs map[string]int
}

type ConnectingNodes struct {
	sync.RWMutex
	ConnectingAddrs []string
}

func (node *node) DumpInfo() {
	log.Info("Node info:")
	log.Info("\t state = ", node.state)
	log.Info(fmt.Sprintf("\t id = 0x%x", node.id))
	log.Info("\t addr = ", node.addr)
	log.Info("\t conn = ", node.conn)
	log.Info("\t cap = ", node.cap)
	log.Info("\t version = ", node.version)
	log.Info("\t services = ", node.services)
	log.Info("\t port = ", node.port)
	log.Info("\t relay = ", node.relay)
	log.Info("\t height = ", node.height)
	log.Info("\t conn cnt = ", node.link.connCnt)
}

func (node *node) IsAddrInNbrList(addr string) bool {
	node.nbrNodes.RLock()
	defer node.nbrNodes.RUnlock()
	for _, n := range node.nbrNodes.List {
		if n.GetState() == HAND || n.GetState() == HANDSHAKE || n.GetState() == ESTABLISH {
			addr := n.GetAddr()
			port := n.GetPort()
			na := addr + ":" + strconv.Itoa(int(port))
			if strings.Compare(na, addr) == 0 {
				return true
			}
		}
	}
	return false
}

func (node *node) SetAddrInConnectingList(addr string) (added bool) {
	node.ConnectingNodes.Lock()
	defer node.ConnectingNodes.Unlock()
	for _, a := range node.ConnectingAddrs {
		if strings.Compare(a, addr) == 0 {
			return false
		}
	}
	node.ConnectingAddrs = append(node.ConnectingAddrs, addr)
	return true
}

func (node *node) RemoveAddrInConnectingList(addr string) {
	node.ConnectingNodes.Lock()
	defer node.ConnectingNodes.Unlock()
	addrs := []string{}
	for i, a := range node.ConnectingAddrs {
		if strings.Compare(a, addr) == 0 {
			addrs = append(node.ConnectingAddrs[:i], node.ConnectingAddrs[i+1:]...)
		}
	}
	node.ConnectingAddrs = addrs
}

func (node *node) UpdateInfo(t time.Time, version uint32, services uint64,
	port uint16, nonce uint64, relay uint8, height uint64) {

	node.UpdateRXTime(t)
	node.id = nonce
	node.version = version
	node.services = services
	node.port = port
	if relay == 0 {
		node.relay = false
	} else {
		node.relay = true
	}
	node.height = uint64(height)
}

func NewNode() *node {
	n := node{
		state: INIT,
		chF:   make(chan func() error),
	}
	runtime.SetFinalizer(&n, rmNode)
	go n.backend()
	return &n
}

func InitNode(pubKey *crypto.PubKey) Noder {
	n := NewNode()
	n.version = PROTOCOLVERSION
	if Parameters.NodeType == SERVICENODENAME {
		n.services = uint64(SERVICENODE)
	} else if Parameters.NodeType == VERIFYNODENAME {
		n.services = uint64(VERIFYNODE)
	}

	if Parameters.MaxHdrSyncReqs <= 0 {
		n.SyncBlkReqSem = MakeSemaphore(MAXSYNCHDRREQ)
		n.SyncHdrReqSem = MakeSemaphore(MAXSYNCHDRREQ)
	} else {
		n.SyncBlkReqSem = MakeSemaphore(Parameters.MaxHdrSyncReqs)
		n.SyncHdrReqSem = MakeSemaphore(Parameters.MaxHdrSyncReqs)
	}

	n.link.port = uint16(Parameters.NodePort)
	n.relay = true
	// TODO is it neccessary to init the rand seed here?
	rand.Seed(time.Now().UTC().UnixNano())

	key, err := pubKey.EncodePoint(true)
	if err != nil {
		log.Error(err)
	}
	err = binary.Read(bytes.NewBuffer(key[:8]), binary.LittleEndian, &(n.id))
	if err != nil {
		log.Error(err)
	}
	log.Info(fmt.Sprintf("Init node ID to 0x%x", n.id))
	n.nbrNodes.init()
	n.KnownAddressList.init()
	n.local = n
	n.publicKey = pubKey
	n.TXNPool.init()
	n.eventQueue.init()
	n.idCache.init()
	n.cachedHashes = make([]Uint256, 0)
	n.local.SetMaxOutboundCnt()
	n.local.SetDefaultMaxPeers()
	n.local.SetGetAddrMax()
	n.nodeDisconnectSubscriber = n.eventQueue.GetEvent("disconnect").Subscribe(events.EventNodeDisconnect, n.NodeDisconnect)
	n.local.headerFirstMode = false
	n.invRequestHashes = make([]Uint256, 0)
	n.RequestedBlockList = make(map[Uint256]time.Time)
	go n.initConnection()
	go n.updateConnection()
	go n.updateNodeInfo()

	return n
}

func (n *node) SetMaxOutboundCnt() {
	if (Parameters.MaxOutboundCnt < MAXOUTBOUNDCNT) && (Parameters.MaxOutboundCnt > 0) {
		n.MaxOutboundCnt = Parameters.MaxOutboundCnt
	} else {
		n.MaxOutboundCnt = MAXOUTBOUNDCNT
	}
}

func (n *node) SetGetAddrMax() {
	if (Parameters.GetAddrMax < GETADDRMAX) && (Parameters.GetAddrMax > 0) {
		n.GetAddrMax = Parameters.GetAddrMax
	} else {
		n.GetAddrMax = GETADDRMAX
	}
}

func (n *node) SetDefaultMaxPeers() {
	if (Parameters.DefaultMaxPeers < DEFAULTMAXPEERS) && (Parameters.DefaultMaxPeers > 0) {
		n.DefaultMaxPeers = Parameters.MaxOutboundCnt
	} else {
		n.DefaultMaxPeers = DEFAULTMAXPEERS
	}
}

func (n *node) GetGetAddrMax() uint {
	return n.GetAddrMax
}

func (n *node) GetMaxOutboundCnt() uint {
	return n.MaxOutboundCnt
}

func (n *node) GetDefaultMaxPeers() uint {
	return n.DefaultMaxPeers
}

func (n *node) NodeDisconnect(v interface{}) {
	if node, ok := v.(*node); ok {
		node.SetState(INACTIVITY)
		conn := node.GetConn()
		conn.Close()
	}
}

func rmNode(node *node) {
	log.Debug(fmt.Sprintf("Remove unused/deuplicate node: 0x%0x", node.id))
}

// TODO pass pointer to method only need modify it
func (node *node) backend() {
	for f := range node.chF {
		f()
	}
}

func (node *node) GetID() uint64 {
	return node.id
}

func (node *node) GetState() uint32 {
	return atomic.LoadUint32(&(node.state))
}

func (node *node) GetConn() net.Conn {
	return node.conn
}

func (node *node) GetPort() uint16 {
	return node.port
}

func (node *node) GetHttpInfoPort() int {
	return int(node.httpInfoPort)
}

func (node *node) SetHttpInfoPort(nodeInfoPort uint16) {
	node.httpInfoPort = nodeInfoPort
}

func (node *node) GetHttpInfoState() bool {
	if node.cap[HTTPINFOFLAG] == 0x01 {
		return true
	} else {
		return false
	}
}

func (node *node) SetHttpInfoState(nodeInfo bool) {
	if nodeInfo {
		node.cap[HTTPINFOFLAG] = 0x01
	} else {
		node.cap[HTTPINFOFLAG] = 0x00
	}
}

func (node *node) GetRelay() bool {
	return node.relay
}

func (node *node) Version() uint32 {
	return node.version
}

func (node *node) Services() uint64 {
	return node.services
}

func (node *node) IncRxTxnCnt() {
	node.rxTxnCnt++
}

func (node *node) GetTxnCnt() uint64 {
	return node.txnCnt
}

func (node *node) GetRxTxnCnt() uint64 {
	return node.rxTxnCnt
}

func (node *node) SetState(state uint32) {
	atomic.StoreUint32(&(node.state), state)
}

func (node *node) GetPubKey() *crypto.PubKey {
	return node.publicKey
}

func (node *node) CompareAndSetState(old, new uint32) bool {
	return atomic.CompareAndSwapUint32(&(node.state), old, new)
}

func (node *node) LocalNode() Noder {
	return node.local
}

func (node *node) GetHeight() uint64 {
	return node.height
}

func (node *node) SetHeight(height uint64) {
	//TODO read/write lock
	node.height = height
}

func (node *node) UpdateRXTime(t time.Time) {
	node.time = t
}

func (node *node) Xmit(message interface{}) error {
	log.Debug()
	var buffer []byte
	var err error
	switch message.(type) {
	case *transaction.Transaction:
		log.Debug("TX transaction message")
		txn := message.(*transaction.Transaction)
		buffer, err = NewTxn(txn)
		if err != nil {
			log.Error("Error New Tx message: ", err)
			return err
		}
		node.txnCnt++
	case *ledger.Block:
		log.Debug("TX block message")
		block := message.(*ledger.Block)
		buffer, err = NewBlock(block)
		if err != nil {
			log.Error("Error New Block message: ", err)
			return err
		}
	case *ConsensusPayload:
		log.Debug("TX consensus message")
		consensusPayload := message.(*ConsensusPayload)
		buffer, err = NewConsensus(consensusPayload)
		if err != nil {
			log.Error("Error New consensus message: ", err)
			return err
		}
	case Uint256:
		log.Debug("TX block hash message")
		hash := message.(Uint256)
		buf := bytes.NewBuffer([]byte{})
		hash.Serialize(buf)
		// construct inv message
		invPayload := NewInvPayload(BLOCK, 1, buf.Bytes())
		buffer, err = NewInv(invPayload)
		if err != nil {
			log.Error("Error New inv message")
			return err
		}
	default:
		log.Warn("Unknown Xmit message type")
		return errors.New("Unknown Xmit message type")
	}

	node.nbrNodes.Broadcast(buffer)

	return nil
}

func (node *node) GetAddr() string {
	return node.addr
}

func (node *node) GetAddr16() ([16]byte, error) {
	var result [16]byte
	ip := net.ParseIP(node.addr).To16()
	if ip == nil {
		log.Error("Parse IP address error\n")
		return result, errors.New("Parse IP address error")
	}

	copy(result[:], ip[:16])
	return result, nil
}

func (node *node) GetTime() int64 {
	t := time.Now()
	return t.UnixNano()
}

func (node *node) GetBookKeeperAddr() *crypto.PubKey {
	return node.publicKey
}

func (node *node) GetBookKeepersAddrs() ([]*crypto.PubKey, uint64) {
	pks := make([]*crypto.PubKey, 1)
	pks[0] = node.publicKey
	var i uint64
	i = 1
	//TODO read lock
	for _, n := range node.nbrNodes.List {
		if n.GetState() == ESTABLISH && n.services != SERVICENODE {
			pktmp := n.GetBookKeeperAddr()
			pks = append(pks, pktmp)
			i++
		}
	}
	return pks, i
}

func (node *node) SetBookKeeperAddr(pk *crypto.PubKey) {
	node.publicKey = pk
}

func (node *node) SyncNodeHeight() {
	for {
		log.Trace("BlockHeight is ", ledger.DefaultLedger.Blockchain.BlockHeight)
		bc := ledger.DefaultLedger.Blockchain
		log.Info("[", len(bc.Index), len(bc.BlockCache), len(bc.Orphans), "]")
		//for x, _ := range node.RequestedBlockList {
		//	log.Info(x)
		//}

		heights, _ := node.GetNeighborHeights()
		log.Trace("others height is ", heights)

		if CompareHeight(uint64(ledger.DefaultLedger.Blockchain.BlockHeight), heights) {
			node.local.SetSyncHeaders(false)

			break
		}

		<-time.After(5 * time.Second)
	}
}

func (node *node) WaitForFourPeersStart() {
	for {
		log.Debug("WaitForFourPeersStart...")
		cnt := node.local.GetNbrNodeCnt()
		if cnt >= MINCONNCNT {
			break
		}
		<-time.After(2 * time.Second)
	}
}

func (node *node) StoreFlightHeight(height uint32) {
	node.flightlock.Lock()
	defer node.flightlock.Unlock()
	node.flightHeights = append(node.flightHeights, height)
}

func (node *node) GetFlightHeightCnt() int {
	return len(node.flightHeights)
}
func (node *node) GetFlightHeights() []uint32 {
	return node.flightHeights
}

func (node *node) RemoveFlightHeightLessThan(h uint32) {
	node.flightlock.Lock()
	defer node.flightlock.Unlock()
	heights := node.flightHeights
	p := len(heights)
	i := 0

	for i < p {
		if heights[i] < h {
			p--
			heights[p], heights[i] = heights[i], heights[p]
		} else {
			i++
		}
	}
	node.flightHeights = heights[:p]
}

func (node *node) RemoveFlightHeight(height uint32) {
	node.flightlock.Lock()
	defer node.flightlock.Unlock()
	log.Debug("height is ", height)
	for _, h := range node.flightHeights {
		log.Debug("flight height ", h)
	}
	node.flightHeights = SliceRemove(node.flightHeights, height)
	for _, h := range node.flightHeights {
		log.Debug("after flight height ", h)
	}
}

func (node *node) GetLastRXTime() time.Time {
	return node.time
}

func (node *node) AddInRetryList(addr string) {
	node.RetryConnAddrs.Lock()
	defer node.RetryConnAddrs.Unlock()
	if node.RetryAddrs == nil {
		node.RetryAddrs = make(map[string]int)
	}
	if _, ok := node.RetryAddrs[addr]; ok {
		delete(node.RetryAddrs, addr)
		log.Debug("remove exsit addr from retry list", addr)
	}
	//alway set retry to 0
	node.RetryAddrs[addr] = 0
	log.Debug("add addr to retry list", addr)
}

func (node *node) RemoveFromRetryList(addr string) {
	node.RetryConnAddrs.Lock()
	defer node.RetryConnAddrs.Unlock()
	if len(node.RetryAddrs) > 0 {
		if _, ok := node.RetryAddrs[addr]; ok {
			delete(node.RetryAddrs, addr)
			log.Debug("remove addr from retry list", addr)
		}
	}
}

func (node *node) Relay(frmnode Noder, message interface{}) error {
	log.Debug()
	if node.LocalNode().IsSyncHeaders() == true {
		return nil
	}
	var buffer []byte
	var err error
	isHash := false
	switch message.(type) {
	case *transaction.Transaction:
		log.Debug("TX transaction message")
		txn := message.(*transaction.Transaction)
		buffer, err = NewTxn(txn)
		if err != nil {
			log.Error("Error New Tx message: ", err)
			return err
		}
		node.txnCnt++
	case *ConsensusPayload:
		log.Debug("TX consensus message")
		consensusPayload := message.(*ConsensusPayload)
		buffer, err = NewConsensus(consensusPayload)
		if err != nil {
			log.Error("Error New consensus message: ", err)
			return err
		}
	case Uint256:
		log.Debug("TX block hash message")
		hash := message.(Uint256)
		isHash = true
		buf := bytes.NewBuffer([]byte{})
		hash.Serialize(buf)
		// construct inv message
		invPayload := NewInvPayload(BLOCK, 1, buf.Bytes())
		buffer, err = NewInv(invPayload)
		if err != nil {
			log.Error("Error New inv message")
			return err
		}
	case *ledger.Block:
		log.Debug("TX block message")
		blkpayload := message.(*ledger.Block)
		buffer, err = NewBlock(blkpayload)
		if err != nil {
			log.Error("Error new block message: ", err)
			return err
		}
	default:
		log.Warn("Unknown Relay message type")
		return errors.New("Unknown Relay message type")
	}

	node.nbrNodes.RLock()
	for _, n := range node.nbrNodes.List {
		if n.state == ESTABLISH && n.relay == true &&
			n.id != frmnode.GetID() {
			if isHash && n.ExistHash(message.(Uint256)) {
				continue
			}
			n.Tx(buffer)
		}
	}
	node.nbrNodes.RUnlock()
	return nil
}

func (node *node) CacheHash(hash Uint256) {
	node.cachelock.Lock()
	defer node.cachelock.Unlock()
	node.cachedHashes = append(node.cachedHashes, hash)
	if len(node.cachedHashes) > MAXCACHEHASH {
		node.cachedHashes = append(node.cachedHashes[:0], node.cachedHashes[1:]...)
	}
}

func (node *node) ExistHash(hash Uint256) bool {
	node.cachelock.Lock()
	defer node.cachelock.Unlock()
	for _, v := range node.cachedHashes {
		if v == hash {
			return true
		}
	}
	return false
}

func (node *node) ExistFlightHeight(height uint32) bool {
	node.flightlock.Lock()
	defer node.flightlock.Unlock()
	for _, v := range node.flightHeights {
		if v == height {
			return true
		}
	}
	return false
}
func (node node) IsSyncHeaders() bool {
	node.flagLock.RLock()
	defer node.flagLock.RUnlock()
	if (node.syncFlag & 0x01) == 0x01 {
		return true
	} else {
		return false
	}
}

func (node *node) SetSyncHeaders(b bool) {
	node.flagLock.Lock()
	defer node.flagLock.Unlock()
	if b == true {
		node.syncFlag = node.syncFlag | 0x01
	} else {
		node.syncFlag = node.syncFlag & 0xFE
	}
}

func (node node) IsSyncFailed() bool {
	node.flagLock.RLock()
	defer node.flagLock.RUnlock()
	if (node.syncFlag & 0x02) == 0x02 {
		return true
	} else {
		return false
	}
}

func (node *node) SetSyncFailed() {
	node.flagLock.Lock()
	defer node.flagLock.Unlock()
	node.syncFlag = node.syncFlag | 0x02
}

func (node *node) NeedSync() bool {
	return node.needSync()
}

func (node *node) needSync() bool {
	heights, _ := node.GetNeighborHeights()
	log.Info("nbr heigh-->", heights, ledger.DefaultLedger.Blockchain.BlockHeight)
	if CompareHeight(uint64(ledger.DefaultLedger.Blockchain.BlockHeight), heights) {
		return false
	}
	return true
}

func (node *node) GetBestHeightNoder() Noder {
	node.nbrNodes.RLock()
	defer node.nbrNodes.RUnlock()
	var bestnode Noder
	for _, n := range node.nbrNodes.List {
		if n.GetState() == ESTABLISH {
			if bestnode == nil {
				if !n.IsSyncFailed() {
					bestnode = n
				}
			} else {
				if (n.GetHeight() > bestnode.GetHeight()) && !n.IsSyncFailed() {
					bestnode = n
				}
			}
		}
	}
	return bestnode
}

func (node *node) StartSync() {
	needSync := node.needSync()
	log.Info("needSync ", needSync)
	if needSync == true {
		currentBlkHeight := uint64(ledger.DefaultLedger.Blockchain.BlockHeight)
		node.NextCheckpoint = node.FindNextHeaderCheckpoint(currentBlkHeight)
		NextCheckpointHeight, err := node.GetNextCheckpointHeight()
		if node.LocalNode().IsSyncHeaders() == false {
			if node.NextCheckpoint != nil && err == nil && currentBlkHeight < NextCheckpointHeight {
				n := node.GetBestHeightNoder()
				hash := ledger.DefaultLedger.Store.GetCurrentBlockHash()
				if node.NextCheckpoint != nil {
					SendMsgSyncHeaders(n, hash)
					node.SetHeaderFirstMode(true)
				} else {
					blocator := ledger.DefaultLedger.Blockchain.BlockLocatorFromHash(&hash)
					var emptyHash Uint256
					SendMsgSyncBlockHeaders(n, blocator, emptyHash)
				}
			}
		}
	}
	node.SetStartSync()
}

func (node *node) isFinishSyncFromSyncNode() bool {
	noders := node.local.GetNeighborNoder()
	for _, n := range noders {
		if n.IsSyncHeaders() == true {
			if uint64(ledger.DefaultLedger.Blockchain.BlockHeight) >= n.GetHeight() {
				return true
			}
		}
	}
	return false
}

func (node *node) CacheInvHash(hash Uint256) {
	node.invHashLock.Lock()
	defer node.invHashLock.Unlock()
	node.invRequestHashes = append(node.invRequestHashes, hash)
	if len(node.invRequestHashes) > MAXINVCACHEHASH {
		node.invRequestHashes = append(node.invRequestHashes[:0], node.invRequestHashes[1:]...)
	}
}

func (node *node) ExistInvHash(hash Uint256) bool {
	node.invHashLock.Lock()
	defer node.invHashLock.Unlock()
	for _, v := range node.invRequestHashes {
		if v == hash {
			return true
		}
	}
	return false
}

func (node *node) DeleteInvHash(hash Uint256) {
	node.invHashLock.Lock()
	defer node.invHashLock.Unlock()
	for i, v := range node.invRequestHashes {
		if v == hash {
			node.invRequestHashes = append(node.invRequestHashes[:i], node.invRequestHashes[i+1:]...)
		}
	}
}

func (node *node) GetHeaderFisrtModeStatus() bool {
	return node.headerFirstMode
}

func (node *node) GetRequestBlockList() map[Uint256]time.Time {
	return node.RequestedBlockList
}

func (node *node) RequestedBlockExisted(hash Uint256) bool {
	node.requestedBlockLock.Lock()
	defer node.requestedBlockLock.Unlock()
	_, ok := node.RequestedBlockList[hash]
	return ok
}

func (node *node) AddRequestedBlock(hash Uint256) {
	node.requestedBlockLock.Lock()
	defer node.requestedBlockLock.Unlock()
	node.RequestedBlockList[hash] = time.Now()
}

func (node *node) ResetRequestedBlock() {
	node.requestedBlockLock.Lock()
	defer node.requestedBlockLock.Unlock()

	node.RequestedBlockList = make(map[Uint256]time.Time)
}

func (node *node) DeleteRequestedBlock(hash Uint256) {
	node.requestedBlockLock.Lock()
	defer node.requestedBlockLock.Unlock()
	_, ok := node.RequestedBlockList[hash]
	if ok == false {
		return
	}
	delete(node.RequestedBlockList, hash)
}

// newCheckpointFromStr parses checkpoints in the '<height>:<hash>' format.
func newCheckpointFromStr(checkpoint string) (Checkpoint, error) {
	parts := strings.Split(checkpoint, ":")
	if len(parts) != 2 {
		return Checkpoint{}, fmt.Errorf("unable to parse "+
			"checkpoint %q -- use the syntax <height>:<hash>",
			checkpoint)
	}

	height, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("unable to parse "+
			"checkpoint %q due to malformed height", checkpoint)
	}
	fmt.Println(height)
	if len(parts[1]) == 0 {
		return Checkpoint{}, fmt.Errorf("unable to parse "+
			"checkpoint %q due to missing hash", checkpoint)
	}
	hashstr := parts[1]
	if err != nil {
		return Checkpoint{}, fmt.Errorf("unable to parse "+
			"checkpoint %q due to malformed hash", checkpoint)
	}
	bhash, _ := HexStringToBytesReverse(hashstr)
	var hash Uint256
	copy(hash[:], bhash)
	log.Trace("hash is ", hash)
	return Checkpoint{
		Height: uint64(height),
		Hash:   hash,
	}, nil
}

func parseCheckpoints(checkpointStrings []string) ([]Checkpoint, error) {
	log.Debug("checkpointStrings ", checkpointStrings)
	if len(checkpointStrings) == 0 {
		return nil, nil
	}
	checkpoints := make([]Checkpoint, len(checkpointStrings))
	for i, cpString := range checkpointStrings {
		checkpoint, err := newCheckpointFromStr(cpString)
		if err != nil {
			return nil, err
		}
		checkpoints[i] = checkpoint
	}
	return checkpoints, nil
}

func (node *node) FindNextHeaderCheckpoint(height uint64) *Checkpoint {
	log.Debug("config.Parameters.AddCheckpoints ", config.Parameters.AddCheckpoints)
	checkpoints, err := parseCheckpoints(config.Parameters.AddCheckpoints)
	if err != nil {
		return nil
	}
	if len(checkpoints) == 0 {
		return nil
	}

	// There is no next checkpoint if the height is already after the final
	// checkpoint.
	finalCheckpoint := &checkpoints[len(checkpoints)-1]
	if height >= finalCheckpoint.Height {
		return nil
	}

	// Find the next checkpoint.
	nextCheckpoint := finalCheckpoint
	var i int
	for i = 0; i <= len(checkpoints)-2; i++ {
		if height < checkpoints[i].Height {
			nextCheckpoint = &checkpoints[i]
			break
		}
	}
	log.Debug("nextCheckpoint height ", nextCheckpoint.Height)
	node.NextCheckpoint = nextCheckpoint
	return nextCheckpoint
}

func (node *node) GetNextCheckpoint() *Checkpoint {
	return node.NextCheckpoint
}

func (node *node) GetNextCheckpointHeight() (uint64, error) {
	if node.NextCheckpoint != nil {
		return node.NextCheckpoint.Height, nil
	} else {
		return 0, errors.New("no next checkpoint any more")
	}
}

func (node *node) GetNextCheckpointHash() (Uint256, error) {
	var hash Uint256
	if node.NextCheckpoint != nil {
		return node.NextCheckpoint.Hash, nil
	} else {
		return hash, errors.New("no next checkpoint any more")
	}
}
func (node *node) SetHeaderFirstMode(b bool) {
	node.headerFirstMode = b
}

func (node *node) FindSyncNode() (Noder, error) {
	noders := node.local.GetNeighborNoder()
	for _, n := range noders {
		if n.IsSyncHeaders() == true {
			return n, nil
		}
	}
	return nil, errors.New("Not in sync mode")
}

func (node *node) SetStartSync() {
	node.IsStartSync = true
}

func (node *node) GetStartSync() bool {
	return node.IsStartSync
}

func (node *node) AcqSyncBlkReqSem() {
	node.SyncBlkReqSem.acquire()
}

func (node *node) RelSyncBlkReqSem() {
	node.SyncBlkReqSem.release()
}
func (node *node) AcqSyncHdrReqSem() {
	node.SyncHdrReqSem.acquire()
}

func (node *node) RelSyncHdrReqSem() {
	node.SyncHdrReqSem.release()
}

func (node *node) SetStartHash(hash Uint256) {
	node.StartHash = hash
}

func (node *node) GetStartHash() Uint256 {
	return node.StartHash
}

func (node *node) SetStopHash(hash Uint256) {
	node.StopHash = hash
}

func (node *node) GetStopHash() Uint256 {
	return node.StopHash
}
