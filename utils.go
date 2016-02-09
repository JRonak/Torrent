package Torrent

import (
	"crypto/sha1"
	bencode "github.com/JRonak/Bencode"
	metadata "github.com/JRonak/Torrent/MetaData"
	"math/rand"
	"strconv"
)

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

func generateInfoHash(m metadata.MetaInfo) ([]byte, error) {
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
