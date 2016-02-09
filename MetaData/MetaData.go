package MetaData

import (
	"errors"
	"github.com/JRonak/Bencode"
	"math"
)

type MetaInfo struct {
	Announce     string     `Bencode:"announce"`
	AnnounceList [][]string `Bencode:"announce-list"`
	Comment      string     `Bencode:"comment"`
	CreatedBy    string     `Bencode:"created by"`
	CreationDate int        `Bencode:"creation date"`
	Encoding     string     `Bencode:"encoding,omitempty"`
	Info         InfoS      `Bencode:"info"`
}

type InfoS struct {
	Length      int    `Bencode:"length,omitempty"`
	Files       []File `Bencode:"files,omitempty"`
	Name        string `Bencode:"name"`
	PieceLength int    `Bencode:"piece length"`
	Pieces      string `Bencode:"pieces"`
}

type File struct {
	Length int      `Bencode:"length"`
	Path   []string `Bencode:"path"`
}

func makeError(str string) error {
	return errors.New("Meta:" + str)
}

func (m *MetaInfo) Size() int {
	return int(legnth(*m))
}

func legnth(m MetaInfo) float64 {
	var f float64
	if m.Info.Length != 0 {
		f += float64(m.Info.Length)
	} else {
		for i := range m.Info.Files {
			f += float64(m.Info.Files[i].Length)
		}
	}
	return f
}

func verifyPieces(m MetaInfo) bool {
	l := legnth(m)
	pl := math.Ceil(l / float64(m.Info.PieceLength))
	x := math.Ceil(float64(len(m.Info.Pieces) / 20))
	if pl == x {
		return true
	}
	return false
}

func verfiy(m MetaInfo) bool {
	if m.Announce == "" {
		return false
	}
	return verifyPieces(m)
}

func BuildMetaData(str string) (MetaInfo, error) {
	m := MetaInfo{}
	if str == "" {
		return m, makeError("Empty input string")
	}
	e := Bencode.Unmarshall(str, &m)
	if e != nil {
		return m, makeError("Error unmarshalling metainfo")
	}
	if !verfiy(m) {
		return m, makeError("Corrupted Torrent")
	}
	return m, nil
}
