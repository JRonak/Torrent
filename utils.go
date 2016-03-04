package Torrent

import (
	"crypto/sha1"
	"fmt"
	bencode "github.com/JRonak/Bencode"
	metadata "github.com/JRonak/Torrent/MetaData"
	"math/rand"
	"strconv"
)

const (
	KB   = 1024
	MB   = 1024 * 1024
	MIN  = 60
	HOUR = 3600
)

func intToTime(time int) string {
	if time >= HOUR {
		return fmt.Sprintf("%2d Hour %2d Mins", time/HOUR, (time%HOUR)/MIN)
	} else if time >= MIN {
		return fmt.Sprintf("%2d Mins %2d Seconds", time/MIN, (time % MIN))
	} else {
		return fmt.Sprintf("%2d Seconds", time)
	}
}

func Address(ip int32, port uint16) string {
	var s string
	for i := 3; i >= 0; i-- {
		x := byte(ip >> uint(i*8))
		s += strconv.Itoa(int(x))
		if i != 0 {
			s += "."
		}
	}
	s += ":" + strconv.Itoa(int(port))
	return s
}

func compare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func generateInfoHash(m *metadata.MetaInfo) ([]byte, error) {
	s, err := bencode.Encode(m.Info)
	if err != nil {
		return nil, err
	}
	sha := sha1.New()
	_, e := sha.Write([]byte(s))
	if e != nil {
		return nil, e
	}
	return sha.Sum(nil), nil
}

func generateRandomPeerId() ([]byte, error) {
	hash := sha1.New()
	str := ""
	i := rand.Int() % 10
	for ; i > 0; i-- {
		str += string(byte(rand.Int()))
	}
	_, e := hash.Write([]byte(str))
	if e != nil {
		return nil, e
	}
	return hash.Sum(nil), nil
}

func intToStr(size int) string {
	if size >= MB {
		return fmt.Sprintf("%4.2fMB", float32(float32(size)/float32(MB)))
	} else if size >= KB {
		return fmt.Sprintf("%4.2fKB", float32(float32(size)/float32(KB)))
	} else {
		return fmt.Sprintf("%d Bs", size)
	}
}
