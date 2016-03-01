package Torrent

import (
	"fmt"
	"github.com/JRonak/Torrent/MetaData"
	"os"
)

type storage interface {
	GetBlock(index, offset int) ([]byte, error)
	PutBlock(data []byte, index int)
}

type localStruct struct {
	data   []byte
	offset int
}

type blob struct {
	channel     chan localStruct
	blobCreated bool
	file        *os.File
	meta        *MetaData.MetaInfo
}

func (this *blob) GetBlock(index, offset int) ([]byte, error) {
	return []byte{}, nil
}

func (this *blob) write() {
	for i := range this.channel {
		_, err := this.file.WriteAt(i.data, int64(this.meta.Info.PieceLength*i.offset))
		if err != nil {
			fmt.Println(err)
		}
	}
}

func (this *blob) PutBlock(data []byte, index int) {
	if this.blobCreated == false {
		this.channel = make(chan localStruct)
		file, err := os.Create(this.meta.Info.Name)
		if err != nil {
			fmt.Println(err)
		}
		this.file = file
		go this.write()
		this.blobCreated = true
		fmt.Println("asdasd")
	}
	this.channel <- localStruct{data: data, offset: index}
}
