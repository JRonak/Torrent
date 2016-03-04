package Torrent

import (
	"bytes"
	"encoding/binary"
	"errors"
	tracker "github.com/JRonak/Torrent/Tracker"
	"log"
	"net"
	"sync"
	"time"
)

const (
	RETRYCOUNT = 1
	BLOCKSIZE  = 65536
	MAXMEMORY  = 1024 * 1024 * 10
)

const (
	CHOKED = iota
	UNCHOKED
	INTERESTED
	UNINTERESTED
	HAVE
	BITFIELD
	REQUEST
	PIECE
)

type Piece struct {
	Id        int           //Index of the piece
	BlockSize int           //Size  of the block
	PieceSize int           //Size  of the piece
	Offset    int           //Offset in the piece
	Data      *bytes.Buffer //Downloaded piece
	Stamp     time.Time
	Lock      sync.Mutex
}

type PieceRequest struct {
	Id     int32 //Index of the piece
	Offset int32 //Offset of the piece
	Size   int32 //Block size to be downloaded or uploaded
}

type PeerClient struct {
	Conn            net.Conn
	PeerId          [20]byte
	manager         *Manager
	piece           Piece
	peerInterested  bool
	peerChoked      bool
	selfInterested  bool
	selfChoked      bool
	piecesAvailable BitArray
	writeChan       chan []byte
}

type HandshakeData struct {
	NameLength byte
	Protocol   [19]byte
	Reserved   [8]byte
	Infohash   [20]byte
	PeerId     [20]byte
}

func (this *PeerClient) Init() {
	this.writeChan = make(chan []byte, 10)
	this.piecesAvailable.bits = 0
	this.piece.Id = -1
	this.piece.Offset = 0
	this.piece.Data = bytes.NewBuffer(nil)
}

func (this *PeerClient) Close() {
	close(this.writeChan)
	this.manager = nil
	if this.Conn != nil {
		this.Conn.Close()
	}
	this.piece.Data.Reset()
}

func (this *PeerClient) Connect(peer tracker.Peer) {
	con, err := net.DialTimeout("tcp", Address(peer.Ip, peer.Port), time.Second*10)
	if err != nil {
		return
	}
	this.Conn = con
	h := HandshakeData{}
	h.Infohash = this.manager.InfoHash
	h.NameLength = 19
	h.PeerId = this.manager.PeerId
	s := "BitTorrent protocol"
	for i := 0; i < 19; i++ {
		h.Protocol[i] = byte(s[i])
	}
	err = binary.Write(this.Conn, binary.LittleEndian, h)
	if err != nil {
		//	log.Println("1:", err)
		return
	}
	data, err := this.read(68, time.Second*10)
	if err != nil {
		//	log.Println("2:", err)
		return
	}
	buf := bytes.NewBuffer(data)
	response := HandshakeData{}
	err = binary.Read(buf, binary.LittleEndian, &response)
	if err != nil {
		//	log.Println("3:", err)
		return
	}
	if response.Infohash != h.Infohash {

		//log.Println("hash fail")
		return
	}
	this.PeerId = response.PeerId
	this.Init()
	if this.manager.registerPeer(this) == false {
		this.Close()
		return
	}
	go this.write()
	//log.Println("connected")
	this.sendBitField()
	this.HandleResponse()
}

func (this *PeerClient) BitField(size int) {
	if size <= 0 {
		return
	}
	data, err := this.read(size, time.Second*5)
	if err != nil {
		return
	}
	this.piecesAvailable.Set(data)
	this.SendInterested()
	this.manager.findPiece(this)
}

//Incomplete function
//Receives 12 byte piece info
func (this *PeerClient) Request() {
	data, err := this.read(12, time.Second*5)
	if err != nil {
		return
	}
	buffer := bytes.NewBuffer(data)
	p := PieceRequest{}
	err = binary.Read(buffer, binary.BigEndian, &p)
	if err != nil {
		return
	}
	this.SendPiece(int(p.Id), int(p.Offset), int(p.Size))
}

func (this *PeerClient) Have() {
	data, err := this.read(4, time.Second*5)
	if err != nil {
		return
	}
	var x int32
	buffer := bytes.NewBuffer(data)
	binary.Read(buffer, binary.BigEndian, &x)
	this.piecesAvailable.LockPiece(int(x))
}

func (this *PeerClient) Piece(size int) {
	data, err := this.read(size, time.Second*15)
	if err != nil {
		return
	}
	buffer := bytes.NewBuffer(data[:8])
	type temp struct {
		Id     int32
		Offset int32
	}
	t := temp{}
	err = binary.Read(buffer, binary.BigEndian, &t)
	if err != nil {
		return
	}
	if int(t.Id) == this.piece.Id && int(t.Offset) == this.piece.Offset {
		this.piece.Data.Write(data[8:])
		this.piece.Offset += len(data[8:])
		this.managePieceDownload()
	}
}

func (this *PeerClient) SendInterested() {
	this.writeChan <- []byte{INTERESTED}
	this.selfInterested = true
}

func (this *PeerClient) SendUninterested() {
	this.writeChan <- []byte{UNINTERESTED}
	this.selfInterested = false
}

func (this *PeerClient) SendChoked() {
	this.writeChan <- []byte{CHOKED}
	this.peerChoked = true
}

func (this *PeerClient) SendUnchoked() {
	this.writeChan <- []byte{UNCHOKED}
	this.peerChoked = false
}

func (this *PeerClient) SendPieceRquest() {
	if !this.selfInterested {
		this.SendInterested()
	}
	p := PieceRequest{
		Size:   int32(this.piece.BlockSize),
		Id:     int32(this.piece.Id),
		Offset: int32(this.piece.Offset)}
	buf := bytes.NewBuffer([]byte{REQUEST})
	err := binary.Write(buf, binary.BigEndian, p)
	if err != nil {
		return
	}
	this.writeChan <- buf.Bytes()
}

func (this *PeerClient) sendHave(index int) {
	buf := bytes.NewBuffer([]byte{HAVE})
	i := int32(index)
	err := binary.Write(buf, binary.BigEndian, i)
	if err != nil {
		return
	}
	this.writeChan <- buf.Bytes()
}

func (this *PeerClient) sendBitField() {
	if this.manager.Stats.AvailablePieces == 0 {
		return
	}
	buf := bytes.NewBuffer([]byte{BITFIELD})
	buf.Write(this.manager.Blocks.AvailablePieces.GetArray())
	this.writeChan <- buf.Bytes()
	buf.Reset()
}

func (this *PeerClient) SendPiece(index, offset, size int) {
	if !this.manager.Blocks.AvailablePieces.IsLocked(index) {
		return
	}
	if this.peerChoked {
		return
	}
	data := this.manager.Storage.GetBlock(index, offset, size)
	type temp struct {
		Id     int32
		Offset int32
	}
	t := temp{Id: int32(index), Offset: int32(offset)}
	buffer := bytes.NewBuffer(nil)
	err := binary.Write(buffer, binary.BigEndian, t)
	if err != nil {
		return
	}
	buffer.Write(data)
	this.writeChan <- buffer.Bytes()
	this.manager.Stats.PieceChan <- UPLOADEDPIECE
}

func (this *PeerClient) write() {
	for data := range this.writeChan {
		length := int32(len(data))
		conn := this.Conn
		err := binary.Write(conn, binary.BigEndian, length)
		if err != nil {
			return
		}
		size := len(data)
		this.manager.Stats.UploadChan <- (size + 4)
		for count := 0; count < size; {
			n, err := conn.Write(data[(count):])
			if err != nil {
				return
			}
			count += n
		}
	}
}

func (this *PeerClient) read(length int, duration time.Duration) ([]byte, error) {
	if length <= 0 {
		return nil, errors.New("Invalid length")
	} else if length > MAXMEMORY {
		log.Println("Requesting high memory!", length)
		this.manager.unresgisterPeer(this)
		return nil, errors.New("Invalid Size")
	}
	conn := this.Conn
	count := length
	databuffer := bytes.NewBuffer(nil)
	for count != 0 {
		err := conn.SetReadDeadline(time.Now().Add(duration))
		if err != nil {
			return nil, err
		}
		buffer := make([]byte, count)
		n, err := conn.Read(buffer)
		if err != nil {
			return nil, err
		}
		n, err = databuffer.Write(buffer[:n])
		if err != nil {
			return nil, err
		}
		count -= n
		this.manager.Stats.DownloadChan <- n
	}
	return databuffer.Bytes(), nil
}

// Handles all the Peer Wire protocol messages
func (this *PeerClient) HandleResponse() {
	for {
		data, err := this.read(5, time.Second*1000)
		if err != nil {
			break
		}
		buffer := bytes.NewBuffer(data[:4])
		var size int32
		binary.Read(buffer, binary.BigEndian, &size)
		switch data[4] {
		case CHOKED:
			this.selfChoked = true
		case UNCHOKED:
			this.selfChoked = false
		case INTERESTED:
			this.peerInterested = true
			if this.peerChoked {
				this.SendUnchoked()
			}
		case UNINTERESTED:
			this.peerInterested = false
		case HAVE:
			this.Have()
		case BITFIELD:
			this.BitField(int(size - 1))
		case REQUEST:
			this.Request()
		case PIECE:
			this.Piece(int(size - 1))
		default:
			break
		}
	}
	this.manager.unresgisterPeer(this)
}

func (this *PeerClient) managePieceDownload() {
	if this.piece.PieceSize == this.piece.Offset {
		data := this.piece.Data.Bytes()
		go this.manager.StorePiece(this.piece.Id, &data)
		this.piece.Lock.Lock()
		this.piece.Id = -1
		this.piece.Data.Reset()
		this.piece.Offset = 0
		this.piece.Lock.Unlock()
		this.manager.Stats.PieceChan <- DELETE_ACTIVE_PEER
		this.manager.findPiece(this)
	} else {
		this.piece.Lock.Lock()
		this.piece.Stamp = time.Now()
		size := this.piece.PieceSize - this.piece.Offset
		if size >= BLOCKSIZE {
			this.piece.BlockSize = BLOCKSIZE
		} else {
			this.piece.BlockSize = size
		}
		this.piece.Lock.Unlock()
		this.SendPieceRquest()
	}
}
