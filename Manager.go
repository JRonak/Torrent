package Torrent

import (
	"crypto/sha1"
	"fmt"
	"github.com/JRonak/Torrent/MetaData"
	"github.com/JRonak/Torrent/Tracker"
	//"log"
	"sync"
	"time"
)

const (
	TIMEOUT = 20
)

const (
	DOWNLOADEDPIECE = iota
	UPLOADEDPIECE
	FAILEDPIECE
	AVAILABLE_PIECES
	ADD_ACTIVE_PEER
	DELETE_ACTIVE_PEER
	ADD_PEER
	DELETE_PEER
)

type PiecesInfo struct {
	totalPieces     int
	AvailablePieces BitArray //Pieces already downloaded
	LockedPieces    BitArray //Pieces being downloaded
	Lock            sync.Mutex
	haveChannel     chan int
}

type TorrentStats struct {
	DownloadedPieces     int
	UploadedPieces       int
	FailedPieces         int
	AvailablePieces      int
	TotalBytesDownloaded int
	TotalBytesUploaded   int
	ActivePeers          int
	TotalPeers           int
	TimeStart            time.Time
	TimeElapsed          int
	PieceChan            chan int
	DownloadChan         chan int
	UploadChan           chan int
}

type Manager struct {
	MetaData *MetaData.MetaInfo
	Blocks   PiecesInfo
	InfoHash [20]byte
	PeerId   [20]byte
	PeerMap  map[[20]byte]*PeerClient
	MapLock  sync.Mutex
	Storage  storage
	Stats    TorrentStats
}

func Init(meta *MetaData.MetaInfo) {
	m := Manager{
		MetaData: meta,
		PeerMap:  make(map[[20]byte]*PeerClient)}
	_blob := new(FileStorage)
	m.Storage = _blob
	m.Blocks.totalPieces = len(m.MetaData.Info.Pieces) / 20
	m.Blocks.AvailablePieces.Set(make([]byte, (m.Blocks.totalPieces/8)+1))
	m.Blocks.LockedPieces.Set(make([]byte, (m.Blocks.totalPieces/8)+1))
	m.Blocks.haveChannel = make(chan int, 10)
	go m.haveBroadcast()
	m.Stats.Init()
	if !m.Storage.Init(meta) {
		m.pieceCheck()
	}
	go m.display()
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
	for i := range this.MetaData.AnnounceList {
		go this.handleTracker(this.MetaData.AnnounceList[i][0])
	}
	go this.handleTracker(this.MetaData.Announce)
	this.peerManager()
}

func (this *Manager) handleTracker(url string) {
	tracker, err := Tracker.BuildTracker(url, this.InfoHash[:], this.MetaData.Size())
	if err != nil {
		return
	}
	for count := 0; count != 4; {
		tracker.SetNumPeers(300)
		err := tracker.Announce()
		if err != nil {
			//fmt.Println(err)
			count++
			continue
		}
		count = 0
		go this.spawnPeer(tracker.Peers)
		<-time.After(time.Second * time.Duration(tracker.Interval()))
	}
}

func (this *Manager) spawnPeer(peers []Tracker.Peer) {
	for i := range peers {
		p := new(PeerClient)
		p.manager = this
		go p.Connect(peers[i])
	}
}

func (this *Manager) registerPeer(p *PeerClient) bool {
	if this.PeerMap[p.PeerId] == nil {
		this.MapLock.Lock()
		this.PeerMap[p.PeerId] = p
		this.MapLock.Unlock()
		this.Stats.PieceChan <- ADD_PEER
		return true
	} else {
		return false
	}
}

func (this *Manager) unresgisterPeer(p *PeerClient) {
	if this == nil {
		return
	}
	this.MapLock.Lock()
	if p.piece.Id != -1 {
		this.Blocks.LockedPieces.UnlockPiece(p.piece.Id)
		this.Stats.PieceChan <- DELETE_ACTIVE_PEER
	}
	p.Close()
	this.Stats.PieceChan <- DELETE_PEER
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
					p.piece.Data.Reset()
					p.piece.Offset = 0
					p.piece.PieceSize = 0
					this.Stats.PieceChan <- DELETE_ACTIVE_PEER
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

func (this *Manager) findPiece(p *PeerClient) {
	if p.selfChoked {
		return
	}
	if this.Stats.ActivePeers > 80 {
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
					this.Stats.PieceChan <- ADD_ACTIVE_PEER
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

func (this *Manager) StorePiece(index int, data *[]byte) {
	if this.Blocks.AvailablePieces.IsLocked(index) {
		this.Stats.PieceChan <- FAILEDPIECE
		return
	}
	hash := sha1.New()
	hash.Write(*data)
	sum := hash.Sum(nil)
	piecehash := []byte(this.MetaData.Info.Pieces[(20 * index):((20 * index) + 20)])
	if !compare(sum, piecehash) {
		this.Stats.PieceChan <- FAILEDPIECE
		return
	}
	if this.Blocks.AvailablePieces.LockPiece(index) {
		this.Storage.PutBlock(data, index)
		this.Stats.PieceChan <- DOWNLOADEDPIECE
		this.Blocks.haveChannel <- index
	} else {
		this.Stats.PieceChan <- FAILEDPIECE
	}
}

func (this *Manager) pieceCheck() {
	totalPieces := this.Blocks.totalPieces
	lastPiece := totalPieces - 1
	pieceSize := this.MetaData.Info.PieceLength
	sha := sha1.New()
	for index := 0; index < totalPieces; index++ {
		if index == lastPiece {
			sha.Write(this.Storage.GetBlock(index, 0, this.MetaData.Size()%pieceSize))
		} else {
			sha.Write(this.Storage.GetBlock(index, 0, pieceSize))
		}
		b := sha.Sum(nil)
		piecehash := []byte(this.MetaData.Info.Pieces[(20 * index):((20 * index) + 20)])
		if compare(piecehash, b) {
			this.Blocks.AvailablePieces.LockPiece(index)
			this.Stats.PieceChan <- AVAILABLE_PIECES
		}
		sha.Reset()
	}
}

func (this *TorrentStats) Init() {
	this.PieceChan = make(chan int, 10)
	this.UploadChan = make(chan int, 40)
	this.DownloadChan = make(chan int, 40)
	this.TimeStart = time.Now()
	go this.downloadStats()
	go this.uploadStats()
	go this.piecesStats()
}

func (this *TorrentStats) downloadStats() {
	for i := range this.DownloadChan {
		this.TotalBytesDownloaded += i
	}
}

func (this *TorrentStats) uploadStats() {
	for i := range this.UploadChan {
		this.TotalBytesUploaded += i
	}
}

func (this *TorrentStats) piecesStats() {
	for i := range this.PieceChan {
		switch i {
		case DOWNLOADEDPIECE:
			this.DownloadedPieces += 1
			this.AvailablePieces += 1
		case UPLOADEDPIECE:
			this.UploadedPieces += 1
		case FAILEDPIECE:
			this.FailedPieces += 1
		case AVAILABLE_PIECES:
			this.AvailablePieces += 1
		case ADD_PEER:
			this.TotalPeers += 1
		case DELETE_PEER:
			this.TotalPeers -= 1
		case ADD_ACTIVE_PEER:
			this.ActivePeers += 1
		case DELETE_ACTIVE_PEER:
			this.ActivePeers -= 1
		}
	}
}

func (this *Manager) haveBroadcast() {
	for i := range this.Blocks.haveChannel {
		for _, m := range this.PeerMap {
			m.sendHave(i)
		}
	}
}

func (this *Manager) display() {
	downloadBytes := float32(0)
	uploadBytes := float32(0)
	tempDownload := float32(0)
	tempUpload := float32(0)
	for {
		this.Stats.TimeElapsed += 1
		tempDownload = float32(this.Stats.TotalBytesDownloaded)
		tempUpload = float32(this.Stats.TotalBytesUploaded)
		fmt.Printf("\rTotal Downloaded: %-8s Total Uploaded: %-8s \nDownloaded: %-2.02f%% |Peers:%d| |Active Peers:%d| \nDownload Speed: %-4.2fkbps Upload Speed: %-4.2fkbps\nTime Elapsed:%s",
			intToStr(this.Stats.TotalBytesDownloaded),
			intToStr(this.Stats.TotalBytesUploaded),
			float32((float32(this.Stats.AvailablePieces)/float32(this.Blocks.totalPieces))*100),
			this.Stats.TotalPeers,
			this.Stats.ActivePeers,
			(tempDownload-downloadBytes)/KB,
			(tempUpload-uploadBytes)/KB,
			intToTime(this.Stats.TimeElapsed))
		downloadBytes = tempDownload
		uploadBytes = tempUpload
		<-time.After(time.Second * 1)
		fmt.Printf("\033[3A")
	}
}

func (this *Manager) test() {
	for {
		<-time.After(time.Second * 1)
		fmt.Println("Active:", this.Stats.ActivePeers, " Total:", this.Stats.TotalPeers)
	}
}
