package Torrent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	tracker "github.com/JRonak/Torrent/Tracker"
	"log"
	"net"
	"runtime"
	"sync"
	"time"
)

const (
	RETRYCOUNT = 1
	BLOCKSIZE  = 16384
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
	interested      bool
	choked          bool
	piecesAvailable BitMap
	writeChan       chan []byte
}

type HandshakeData struct {
	NameLength byte
	Protocol   [19]byte
	Reserved   [8]byte
	Infohash   [20]byte
	PeerId     [20]byte
}

func (this *PeerClient) Intialize(peer tracker.Peer) {
	this.writeChan = make(chan []byte, 10)
	this.piece.Id = -1
	this.piece.Offset = 0
	this.piece.Data = bytes.NewBuffer(nil)
	con, err := net.DialTimeout("tcp", Address(peer.Ip, peer.Port), time.Second*5)
	if err != nil {
		runtime.Goexit()
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
		log.Println("PeerClient - Handshake:" + err.Error())
		runtime.Goexit()
		return
	}
	data, err := this.read(68, time.Second*5)
	if err != nil {
		runtime.Goexit()
		return
	}
	buf := bytes.NewBuffer(data)
	response := HandshakeData{}
	err = binary.Read(buf, binary.LittleEndian, &response)
	if err != nil {
		log.Println("Peer Client Handshake:" + err.Error())
		runtime.Goexit()
		return
	}
	if response.Infohash != h.Infohash {
		log.Println("PeerClient Handshake failed")
		runtime.Goexit()
	}
	this.PeerId = response.PeerId
	if this.manager.registerPeer(this) == false {
		this.Conn.Close()
		runtime.Goexit()
	}
	go this.write()
	this.SendInterested()
	this.HandleResponse()
}

func (this *PeerClient) BitField(size int) {
	if size <= 0 {
		return
	}
	data, err := this.read(size, time.Second*5)
	if err != nil {
		fmt.Println(err)
		return
	}
	this.piecesAvailable.Set(data)
	this.manager.findPiece(this)
}

//Incomplete function
//Receives 12 byte piece info
func (this *PeerClient) Request() {
	data, err := this.read(12, time.Second*5)
	if err != nil {
		fmt.Println(err)
		return
	}
	buffer := bytes.NewBuffer(data)
	p := PieceRequest{}
	err = binary.Read(buffer, binary.BigEndian, &p)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(p)
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

func (this *PeerClient) read(length int, duration time.Duration) ([]byte, error) {
	if length <= 0 {
		return nil, errors.New("Invalid length")
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
	}
	return databuffer.Bytes(), nil
}

func (this *PeerClient) SendInterested() {
	this.writeChan <- []byte{2}
	this.interested = true
}

func (this *PeerClient) SendPieceRquest() {
	if !this.interested {
		this.SendInterested()
	}
	p := PieceRequest{
		Size:   int32(this.piece.BlockSize),
		Id:     int32(this.piece.Id),
		Offset: int32(this.piece.Offset)}
	buf := bytes.NewBuffer([]byte{6})
	err := binary.Write(buf, binary.BigEndian, p)
	if err != nil {
		fmt.Println("SendPiece:", err.Error())
		return
	}
	this.writeChan <- buf.Bytes()
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
		for count := 0; count < size; {
			n, err := conn.Write(data[(count):])
			if err != nil {
				return
			}
			count += n
		}
	}
}

func (this *PeerClient) kill() {
	this.Conn.Close()
	close(this.writeChan)
}

// Handles all the Peer Wire protocol messages
func (this *PeerClient) HandleResponse() {
	for {
		data, err := this.read(5, time.Second*1000)
		if err != nil {
			break
		}
		if len(data) != 5 {
			fmt.Println("unknown input")
			continue
		}
		buffer := bytes.NewBuffer(data[:4])
		var size int32
		binary.Read(buffer, binary.BigEndian, &size)
		switch data[4] {
		case CHOKED:
			this.peerChoked = true
		case UNCHOKED:
			this.peerChoked = false
		case INTERESTED:
			this.peerInterested = true
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
			this.Conn.Close()
		}
	}
	this.manager.unresgisterPeer(this)
}

func (this *PeerClient) managePieceDownload() {
	if this.piece.PieceSize == this.piece.Offset {
		this.manager.StorePiece(this.piece.Id, this.piece.Data.Bytes())
		this.piece.Lock.Lock()
		this.piece.Id = -1
		this.piece.Data.Reset()
		this.piece.Offset = 0
		this.piece.Lock.Unlock()
	} else {
		size := this.piece.PieceSize - this.piece.Offset
		if size >= BLOCKSIZE {
			this.piece.BlockSize = BLOCKSIZE
		} else {
			this.piece.BlockSize = size
		}
		this.SendPieceRquest()
	}
}
