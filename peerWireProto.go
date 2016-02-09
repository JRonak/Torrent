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
	"sync/atomic"
	"time"
)

const (
	retryCount = 1
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

var (
	currentPeers int32
)

func init() {
	currentPeers = 0
	go trace()
}

func trace() {
	for {
		<-time.After(time.Second * 2)
		fmt.Println("Current Peers", atomic.LoadInt32(&currentPeers))
	}
}

type Piece struct {
	Id     int    //Index of the piece
	Size   int    //Size of the piece
	offset int    //Offset in the piece
	Data   []byte //Downloaded piece
}

type PieceRequest struct {
	Id     int32 //Index of the piece
	Offset int32 //Offset of the piece
	Size   int32 //Block size to be downloaded or uploaded
}

type PeerClient struct {
	Conn            net.Conn
	Peer            tracker.Peer
	manager         *Manager
	piece           Piece
	peerInterested  bool
	peerChoked      bool
	interested      bool
	choked          bool
	piecesAvailable []byte
}

type HandshakeData struct {
	NameLength byte
	Protocol   [19]byte
	Reserved   [8]byte
	Infohash   [20]byte
	PeerId     [20]byte
}

func (this *PeerClient) Intialize() {
	this.piece.Id = -1
	this.piecesAvailable = make([]byte, 56)
	con, err := net.DialTimeout("tcp", Address(this.Peer.Ip, this.Peer.Port), time.Second*5)
	if err != nil {
		runtime.Goexit()
	}
	this.Conn = con
	this.Handshake()
}

func (this *PeerClient) Handshake() {
	h := HandshakeData{}
	h.Infohash = this.manager.InfoHash
	h.NameLength = 19
	h.PeerId = this.manager.PeerId
	s := "BitTorrent protocol"
	for i := 0; i < 19; i++ {
		h.Protocol[i] = byte(s[i])
	}
	err := binary.Write(this.Conn, binary.LittleEndian, h)
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
	this.piecesAvailable = data
	this.findPieceToDownload()
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
	if len(this.piecesAvailable) > int((x / 8)) {
		this.piecesAvailable[x/8] |= 1 << (7 - uint(x%8))
	} else {
		fmt.Println("Have message out of bound:", x, " len of the pieces", len(this.piecesAvailable))
	}
}

func (this *PeerClient) findPieceToDownload() {
	t := false
	fmt.Println("locked")
	for i := range this.manager.Blocks.AvailablePieces {
		x := this.manager.Blocks.AvailablePieces[i] ^ this.piecesAvailable[i]
		if x != 0 {
			x ^= this.manager.Blocks.LockedPieces[i]
			if x != 0 {
				for j := 0; j < 8; j++ {
					if x&(1<<uint(7-j)) != 0 {
						this.manager.Blocks.LockedPieces[i] |= 1 << uint(7-j)
						fmt.Println(this.manager.Blocks.LockedPieces[i])
						this.piece.Id = i*8 + j
						this.SendPieceRquest()
						t = true
						break
					}
				}
				if t {
					break
				}
			}
		}
	}
	fmt.Println("here")

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
	fmt.Println("recived piece", t)
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
	this.write([]byte{2})
	this.interested = true
}

func (this *PeerClient) SendPieceRquest() {
	if !this.interested {
		this.SendInterested()
	}
	p := PieceRequest{
		Size:   16384,
		Id:     int32(this.piece.Id),
		Offset: int32(this.piece.offset)}
	buf := bytes.NewBuffer([]byte{6})
	err := binary.Write(buf, binary.BigEndian, p)
	if err != nil {
		fmt.Println("SendPiece:", err.Error())
		return
	}
	this.write(buf.Bytes())
}

func (this *PeerClient) write(data []byte) {
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

// Handles all the Peer Wire protocol messages
func (this *PeerClient) HandleResponse() {
	atomic.AddInt32(&currentPeers, 1)
	for {
		data, err := this.read(5, time.Second*1000)
		if err != nil {
			atomic.AddInt32(&currentPeers, -1)
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
			runtime.Goexit()
		}
	}
}
