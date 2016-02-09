package Torrent

import (
	"fmt"
	"github.com/JRonak/Torrent/MetaData"
	"github.com/JRonak/Torrent/Tracker"
	"sync"
	"time"
)

type PiecesInfo struct {
	Lock            sync.Mutex
	AvailablePieces []byte //Pieces already downloaded
	LockedPieces    []byte //Pieces being downloaded
}

type Manager struct {
	MetaData MetaData.MetaInfo
	Blocks   PiecesInfo
	InfoHash [20]byte
	PeerId   [20]byte
	PeerMap  map[Tracker.Peer]bool
	MapLock  sync.Mutex
}

func Init(meta MetaData.MetaInfo) {
	m := Manager{
		MetaData: meta,
		PeerMap:  make(map[Tracker.Peer]bool)}
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
	this.Blocks.AvailablePieces = make([]byte, (len(this.MetaData.Info.Pieces)/160)+1)
	fmt.Println(len(this.Blocks.AvailablePieces))
	this.Blocks.LockedPieces = this.Blocks.AvailablePieces
	this.handleTracker(this.MetaData.Announce)
}

func (this *Manager) handleTracker(url string) {
	tracker, err := Tracker.BuildTracker(url, this.InfoHash[:], this.MetaData.Size())
	if err != nil {
		fmt.Println(err)
		return
	}
	tracker.SetNumPeers(20)
	count := 0
	for count != 4 {
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
		this.MapLock.Lock()
		if this.PeerMap[peers[i]] == false {
			this.PeerMap[peers[i]] = true
			p := PeerClient{}
			p.Peer = peers[i]
			p.manager = this
			go p.Intialize()
		}
		this.MapLock.Unlock()
	}
}
