package Torrent

import (
	"bytes"
	"github.com/JRonak/Torrent/MetaData"
	"log"
	"os"
)

type FileStorage struct {
	files         []*os.File
	meta          *MetaData.MetaInfo
	MultipleFiles bool
	writeChan     chan localStruct
}

func (this *FileStorage) Init(meta *MetaData.MetaInfo) bool {
	this.meta = meta
	this.writeChan = make(chan localStruct, 10)
	go this.write()
	if meta.Info.Files != nil {
		this.MultipleFiles = true
		return this.multiInit()
	} else {
		this.MultipleFiles = false
		return this.singleInit()
	}
}

func (this *FileStorage) singleInit() bool {
	if this.isFileExists(this.meta.Info.Name) {
		file, err := os.OpenFile(this.meta.Info.Name, os.O_RDWR, 0766)
		if err != nil {
			log.Println(err)
			return false
		}
		this.files = append(this.files, file)
		return false
	}
	file := this.createFile(this.meta.Info.Name, this.meta.Size())
	this.files = append(this.files, file)
	return true
}

func (this *FileStorage) multiInit() bool {
	if !this.isDirExists(this.meta.Info.Name) {
		this.createDir(this.meta.Info.Name)
	}
	fileCreated := true
	for _, f := range this.meta.Info.Files {
		length := len(f.Path)
		path := this.meta.Info.Name
		for i := 0; i < length-1; i++ {
			path += "/" + f.Path[i]
		}
		filePath := path + "/" + f.Path[length-1]
		if !this.isDirExists(path) {
			this.createDir(path)
		}
		if !this.isFileExists(filePath) {
			file := this.createFile(filePath, f.Length)
			this.files = append(this.files, file)
		} else {
			file, err := os.OpenFile(filePath, os.O_RDWR, 0777)
			if err != nil {
				log.Println(err)
				continue
			}
			this.files = append(this.files, file)
			fileCreated = false
		}
	}
	return fileCreated
}

func (this *FileStorage) createFile(name string, size int) *os.File {
	f, err := os.Create(name)
	if err != nil {
		log.Println(err)
		return nil
	}
	buf := make([]byte, MB)
	for size >= MB {
		n, err := f.Write(buf)
		if err != nil {
			log.Println(err)
			return nil
		}
		size -= n
	}
	if size > 0 {
		_, err := f.Write(buf[:size])
		if err != nil {
			log.Println(err)
			return nil
		}
	}
	return f
}

func (this *FileStorage) createDir(name string) {
	os.MkdirAll(name, 0766)
}

func (this *FileStorage) isDirExists(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		log.Println(err)
		return false
	}
	if stat.IsDir() {
		return true
	}
	return false
}

func (this *FileStorage) isFileExists(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		log.Println(err)
		return false
	}
	if stat.IsDir() {
		return false
	}
	return true
}

//Generic function
func writeFile(file *os.File, data *[]byte, offset int) {
	if file != nil {
		_, err := file.WriteAt(*data, int64(offset))
		if err != nil {
			log.Println(err)
		}
	}
}

func (this *FileStorage) writeMultiple(piece localStruct) {
	pSize := len(*piece.data)
	pStart := piece.offset * this.meta.Info.PieceLength
	pEnd := pStart + pSize
	pCurr := pStart
	pOffset := 0
	fOffset := 0
	for i, file := range this.files {
		fSize := this.meta.Info.Files[i].Length
		fEnd := fOffset + fSize

		if fOffset <= pStart && pStart <= fEnd {
			if pEnd <= fEnd {
				data := *piece.data
				data = data[pOffset:]
				writeFile(file, &data, (pCurr - fOffset))
				break
			} else {
				data := *piece.data
				data = data[pOffset:((fEnd - pCurr) + pOffset)]
				writeFile(file, &data, (pCurr - fOffset))
				pOffset = (fEnd - pCurr)
				pCurr = fEnd
			}
		}
		fOffset = fEnd
	}
}

func (this *FileStorage) write() {
	for i := range this.writeChan {
		if !this.MultipleFiles {
			if len(this.files) == 0 {
				log.Fatal("file not added")
			}
			writeFile(this.files[0], i.data, this.meta.Info.PieceLength*i.offset)
		} else {
			this.tempWriteMulti(i)
		}
	}
}

func (this *FileStorage) PutBlock(data *[]byte, index int) {
	this.writeChan <- localStruct{data: data, offset: index}
}

func (this *FileStorage) GetBlock(index, offset, size int) []byte {
	if this.MultipleFiles {
		return this.tempReadMultiple(index, offset, size)
	} else {
		return this.getBlockSingle(index, offset, size)
	}
}

func (this *FileStorage) getBlockSingle(index, offset, size int) []byte {
	file := this.files[0]
	data := make([]byte, size)
	n, err := file.ReadAt(data, int64((index*this.meta.Info.PieceLength)+offset))
	if err != nil {
		log.Println(err)
		return nil
	}
	return data[:n]
}

func (this *FileStorage) getBlockMulti(index, offset, size int) []byte {
	pStart := index * this.meta.Info.PieceLength
	pEnd := pStart + size
	pCurr := pStart
	fCurr := 0
	buff := bytes.NewBuffer(nil)
	for i, file := range this.files {
		fSize := this.meta.Info.Files[i].Length
		fEnd := fCurr + fSize
		if fCurr <= pStart && pStart < fEnd {
			if pEnd <= fEnd {
				data := make([]byte, (pEnd - pCurr))
				file.ReadAt(data, int64(pCurr-fCurr))
				buff.Write(data)
				break
			} else {
				data := make([]byte, fEnd-pCurr)
				file.ReadAt(data, int64(pCurr-fCurr))
				buff.Write(data)
				pCurr = fEnd
			}
		}
		fCurr = fEnd
	}
	return buff.Bytes()
}

func (this *FileStorage) tempWriteMulti(piece localStruct) {
	pieceSize := this.meta.Info.PieceLength
	pieceStart := pieceSize * piece.offset
	pieceEnd := pieceStart + pieceSize
	pieceCurrent := pieceStart
	fileStart := 0
	for i, file := range this.files {
		fileEnd := fileStart + this.meta.Info.Files[i].Length
		if fileStart <= pieceCurrent && pieceCurrent < fileEnd {
			if pieceEnd <= fileEnd {
				data := *piece.data
				data = data[pieceCurrent-pieceStart:]
				_, err := file.WriteAt(data, int64(pieceCurrent-fileStart))
				if err != nil {
					log.Println(err)
				}
				break
			} else {
				data := *piece.data
				dataStart := pieceCurrent - pieceStart
				data = data[dataStart : dataStart+(fileEnd-pieceCurrent)]
				_, err := file.WriteAt(data, int64(pieceCurrent-fileStart))
				if err != nil {
					log.Println(err)
				}
				pieceCurrent += (fileEnd - pieceCurrent)
			}
		}
		fileStart = fileEnd
	}
}

func (this *FileStorage) tempReadMultiple(index, offset, size int) []byte {
	pieceSize := this.meta.Info.PieceLength
	pieceStart := pieceSize*index + offset
	pieceEnd := pieceStart + size
	pieceCurrent := pieceStart
	fileStart := 0
	buffer := bytes.NewBuffer(nil)
	for i, file := range this.files {
		fileEnd := fileStart + this.meta.Info.Files[i].Length
		if fileStart <= pieceCurrent && pieceCurrent < fileEnd {
			if pieceEnd <= fileEnd {
				data := make([]byte, pieceEnd-pieceCurrent)
				file.ReadAt(data, int64(pieceCurrent-fileStart))
				buffer.Write(data)
				break
			} else {
				data := make([]byte, fileEnd-pieceCurrent)
				file.ReadAt(data, int64(pieceCurrent-fileStart))
				buffer.Write(data)
				pieceCurrent += (fileEnd - pieceCurrent)
			}
		}
		fileStart = fileEnd
	}
	return buffer.Bytes()
}
