package Torrent

import (
	"fmt"
	"github.com/JRonak/Torrent/MetaData"
	"os"
)

type storage interface {
	Init(*MetaData.MetaInfo) bool
	GetBlock(index, offset, size int) []byte
	PutBlock(data *[]byte, index int)
}

type localStruct struct {
	data   *[]byte
	offset int
}

type blob struct {
	channel chan localStruct
	file    *os.File
	meta    *MetaData.MetaInfo
}

//Returns status if file created
func (this *blob) Init(meta *MetaData.MetaInfo) bool {
	this.meta = meta
	this.channel = make(chan localStruct, 10)
	go this.write()
	f, err := os.OpenFile(this.meta.Info.Name, os.O_RDWR, os.ModeAppend)
	if err == nil {
		this.file = f
		return false
	} else {
		f, err = os.Create(meta.Info.Name)
		if err != nil {
			fmt.Println("blob: ", err)
			return false
		}
		this.file = f
		this.fill()
		return true
	}
}

func (this *blob) fill() {
	size := this.meta.Size()
	max := 1024 * 1024
	offset := 0
	data := make([]byte, max)
	for {
		if size <= max {
			break
		}
		_, err := this.file.WriteAt(data, int64(offset*max))
		if err != nil {
			fmt.Println("blob: ", err)
			return
		}
		size -= max
		offset += 1
	}
	if size > 0 {
		_, err := this.file.WriteAt(data[:size], int64(offset*max))
		if err != nil {
			fmt.Println("blob:", err)
		}
	}
	data = nil
}

func (this *blob) GetBlock(index, offset, size int) []byte {
	data := make([]byte, size)
	n, err := this.file.ReadAt(data, int64((index*this.meta.Info.PieceLength)+offset))
	if err != nil {
		fmt.Println(err)
		return nil
	}
	return data[:n]
}

func (this *blob) write() {
	for i := range this.channel {
		_, err := this.file.WriteAt(*i.data, int64(this.meta.Info.PieceLength*i.offset))
		if err != nil {
			fmt.Println(err)
		}
	}
}

func (this *blob) PutBlock(data *[]byte, index int) {
	this.channel <- localStruct{data: data, offset: index}
}
