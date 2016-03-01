package Torrent

import (
	"fmt"
	"github.com/JRonak/Torrent/MetaData"
	"github.com/JRonak/Torrent/Tracker"
	"sync"
	"time"
)

type PiecesInfo struct {
	totalPieces     int
	Lock            sync.Mutex
	AvailablePieces []byte //Pieces already downloaded
	LockedPieces    []byte //Pieces being downloaded
}

type Manager struct {
	MetaData MetaData.MetaInfo
	Blocks   PiecesInfo
	InfoHash [20]byte
	PeerId   [20]byte
	PeerMap  map[Tracker.Peer]*PeerClient
	MapLock  sync.Mutex
	s        storage
}

func Init(meta MetaData.MetaInfo) {
	m := Manager{
		MetaData: meta,
		PeerMap:  make(map[Tracker.Peer]*PeerClient)}
	_blob := new(blob)
	_blob.meta = &meta
	m.s = _blob
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
	this.Blocks.AvailablePieces = make([]byte, (this.Blocks.totalPieces/8)+1)
	this.Blocks.LockedPieces = make([]byte, (this.Blocks.totalPieces/8)+1)
	this.handleTracker(this.MetaData.Announce)
}

func (this *Manager) handleTracker(url string) {
	tracker, err := Tracker.BuildTracker(url, this.InfoHash[:], this.MetaData.Size())
	if err != nil {
		fmt.Println(err)
		return
	}
	count := 0
	tracker.SetNumPeers(500)
	for count != 4 {
		err := tracker.Announce()
		if err != nil {
			count++
			fmt.Println(err)
			continue
		}
		count = 0
		go this.check()
		go this.spawnPeer(tracker.Peers)
		<-time.After(time.Second * time.Duration(tracker.Interval()))
	}
}

func (this *Manager) spawnPeer(peers []Tracker.Peer) {
	for i := range peers {
		if this.PeerMap[peers[i]] == nil {
			p := PeerClient{}
			p.Peer = peers[i]
			p.manager = this
			go p.Intialize()
		}
	}
}

func (this *Manager) registerPeer(p *PeerClient) {
	this.MapLock.Lock()
	this.PeerMap[p.Peer] = p
	this.MapLock.Unlock()
}

func (this *Manager) check() {
	for {
		<-time.After(time.Second * 20)
		for x := range this.PeerMap {
			p := this.PeerMap[x]
			if p.piece.Id != -1 {
				t := p.piece.stamp
				t.Add(time.Second * 20)
				if time.Now().After(t) {
					id := p.piece.Id
					p.piece.Id = -1
					go this.findPeer(id, p)
				}
			}
		}
	}
}

func (this *Manager) findPeer(id int, dont *PeerClient) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("recovered")
		}
	}()
	x := false
	for iter := 0; iter < 4; iter++ {
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
					p.piece.stamp = time.Now()
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
}

func (this *Manager) findPiece(p *PeerClient) {
	m := &this.Blocks
	size := m.totalPieces
	m.Lock.Lock()
	avail := m.AvailablePieces
	locked := m.LockedPieces
	t := false
	for i := range avail {
		for j := 0; j < 8; j++ {
			y := (i * 8) + j
			if y >= size {
				break
			}
			availBit := avail[i] & (1 << uint(7-j))
			lockedBit := locked[i] & (1 << uint(7-j))
			pieceBit := p.piecesAvailable[i] & (1 << uint(7-j))
			if availBit == 0 && lockedBit == 0 && pieceBit != 0 {
				if p.piece.Id != -1 {
					break
				}
				p.piece.Id = i*8 + j
				if p.piece.Id == (size - 1) {
					p.piece.Size = this.MetaData.Size() % this.MetaData.Info.PieceLength
					fmt.Println(p.piece.Size, "SIze")
				} else {
					p.piece.Size = this.MetaData.Info.PieceLength
				}
				p.piece.stamp = time.Now()
				p.SendPieceRquest()
				m.LockedPieces[i] |= (1 << uint(7-j))
				t = true
				break
			}
		}
		if t {
			break
		}
	}
	m.Lock.Unlock()
}
