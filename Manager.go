package Torrent

import (
	"crypto/sha1"
	"fmt"
	"github.com/JRonak/Torrent/MetaData"
	"github.com/JRonak/Torrent/Tracker"
	"sync"
	"sync/atomic"
	"time"
)

const (
	TIMEOUT = 20
)

var (
	PIECES int32
	PEERS  int32
)

func init() {
	go trace()
}

func trace() {
	for {
		<-time.After(time.Second * 2)
		fmt.Printf("\rPeers:%d Pieces%d", atomic.LoadInt32(&PEERS), atomic.LoadInt32(&PIECES))
	}
}

type PiecesInfo struct {
	totalPieces     int
	AvailablePieces BitMap //Pieces already downloaded
	LockedPieces    BitMap //Pieces being downloaded
	Lock            sync.Mutex
}

type Manager struct {
	MetaData MetaData.MetaInfo
	Blocks   PiecesInfo
	InfoHash [20]byte
	PeerId   [20]byte
	PeerMap  map[[20]byte]*PeerClient
	MapLock  sync.Mutex
	Storage  storage
}

func Init(meta MetaData.MetaInfo) {
	m := Manager{
		MetaData: meta,
		PeerMap:  make(map[[20]byte]*PeerClient)}
	_blob := new(blob)
	_blob.meta = &meta
	m.Storage = _blob
	m.manage()
}

func (this *Manager) manage() {
	hash, err := generateInfoHash(this.MetaData)
	if err != nil {
		fmt.Println(err)
		return
	}
	for i := range hash {
		this.InfoHash[i] = hash[i]
	}
	peerId, err := generateRandomPeerId()
	if err != nil {
		fmt.Println(err)
		return
	}
	for i := range peerId {
		this.PeerId[i] = peerId[i]
	}
	this.Blocks.totalPieces = len(this.MetaData.Info.Pieces) / 20
	fmt.Println(this.Blocks.totalPieces)
	this.Blocks.AvailablePieces.Set(make([]byte, (this.Blocks.totalPieces/8)+1))
	this.Blocks.LockedPieces.Set(make([]byte, (this.Blocks.totalPieces/8)+1))
	fmt.Println(this.Blocks.AvailablePieces.GetByte(0))
	go this.peerManager()
	for i := range this.MetaData.AnnounceList[0] {
		go this.handleTracker(this.MetaData.AnnounceList[0][i])
	}
	this.handleTracker(this.MetaData.Announce)
}

func (this *Manager) handleTracker(url string) {
	tracker, err := Tracker.BuildTracker(url, this.InfoHash[:], this.MetaData.Size())
	if err != nil {
		fmt.Println(err)
		return
	}
	for count := 0; count != 4; {
		tracker.SetNumPeers(300)
		err := tracker.Announce()
		if err != nil {
			count++
			fmt.Println(err)
			continue
		}
		count = 0
		go this.spawnPeer(tracker.Peers)
		<-time.After(time.Second * time.Duration(tracker.Interval()))
	}
}

func (this *Manager) spawnPeer(peers []Tracker.Peer) {
	for i := range peers {
		p := PeerClient{}
		p.manager = this
		go p.Intialize(peers[i])
	}
}

func (this *Manager) registerPeer(p *PeerClient) bool {
	if this.PeerMap[p.PeerId] == nil {
		this.MapLock.Lock()
		atomic.AddInt32(&PEERS, 1)
		this.PeerMap[p.PeerId] = p
		this.MapLock.Unlock()
		return true
	} else {
		return false
	}
}

func (this *Manager) unresgisterPeer(p *PeerClient) {
	this.MapLock.Lock()
	if p.piece.Id != -1 {
		this.Blocks.LockedPieces.UnlockPiece(p.piece.Id)
	}
	atomic.AddInt32(&PEERS, -1)
	delete(this.PeerMap, p.PeerId)
	this.MapLock.Unlock()
}

func (this *Manager) peerManager() {
	count := 0
	for {
		count += 1
		<-time.After(time.Second * 5)
		this.MapLock.Lock()
		for x := range this.PeerMap {
			p := this.PeerMap[x]
			p.piece.Lock.Lock()
			if p.piece.Id != -1 {
				t := p.piece.Stamp
				t = t.Add(time.Second * TIMEOUT)
				if time.Now().After(t) {
					this.Blocks.LockedPieces.UnlockPiece(p.piece.Id)
					p.piece.Id = -1
				}
				p.piece.Lock.Unlock()
			} else {
				p.piece.Lock.Unlock()
				this.findPiece(p)
			}
		}
		this.MapLock.Unlock()
	}
}

/*
func (this *Manager) replacePeer(id int, dont *PeerClient) {
	x := false
	for {
		if (this.Blocks.AvailablePieces[id/8] & (1 << uint(7-id%8))) == 0 {
			for i := range this.PeerMap {

				p := this.PeerMap[i]
				if p.piece.Id != -1 {
					continue
				}
				if (p.piecesAvailable[id/8] & (1 << uint(7-(id%8)))) > 0 {
					p.piece.Id = id
					if id == (this.Blocks.totalPieces - 1) {
						p.piece.Size = this.MetaData.Size() % this.MetaData.Info.PieceLength
						fmt.Println(p.piece.Size, "SIze")
					} else {
						p.piece.Size = this.MetaData.Info.PieceLength
					}
					fmt.Println("found ", id)
					p.piece.Stamp = time.Now()
					p.SendPieceRquest()
					x = true
					break
				}
			}
			if !x {
				fmt.Println("retry:", id)
				<-time.After(time.Second * 20)
			} else {
				break
			}
		}
	}
}*/

func (this *Manager) findPiece(p *PeerClient) {
	if p.peerChoked {
		return
	}
	blocks := &this.Blocks
	blocks.Lock.Lock()
	status := false
	for index := 0; index < blocks.totalPieces; index++ {
		if !blocks.AvailablePieces.IsLocked(index) && !blocks.LockedPieces.IsLocked(index) && p.piecesAvailable.IsLocked(index) {
			p.piece.Lock.Lock()
			if p.piece.Id == -1 {
				if blocks.LockedPieces.LockPiece(index) {
					p.piece.Id = index
					if index == (blocks.totalPieces - 1) {
						p.piece.PieceSize = this.MetaData.Size() % this.MetaData.Info.PieceLength
					} else {
						p.piece.PieceSize = this.MetaData.Info.PieceLength
					}
					p.piece.Stamp = time.Now()
					status = true
				}
			}
			p.piece.Lock.Unlock()
		}
		if status {
			p.managePieceDownload()
			break
		}
	}
	blocks.Lock.Unlock()
}

func (this *Manager) StorePiece(index int, data []byte) {
	if this.Blocks.AvailablePieces.IsLocked(index) {
		return
	}
	hash := sha1.New()
	hash.Write(data)
	sum := hash.Sum(nil)
	piecehash := []byte(this.MetaData.Info.Pieces[(20 * index):((20 * index) + 20)])
	for i := range sum {
		if sum[i] != piecehash[i] {
			this.Blocks.LockedPieces.UnlockPiece(index)
			break
		}
	}
	if this.Blocks.AvailablePieces.LockPiece(index) {
		this.Storage.PutBlock(data, index)
		atomic.AddInt32(&PIECES, 1)
	}
}
