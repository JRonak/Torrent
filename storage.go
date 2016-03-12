package Torrent

import (
	"github.com/JRonak/Torrent/MetaData"
)

type storage interface {
	Init(*MetaData.MetaInfo) bool
	GetBlock(index, offset, size int) []byte
	PutBlock(data *[]byte, index int)
}
