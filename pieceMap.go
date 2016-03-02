package Torrent

import (
	"sync"
)

type BitMap struct {
	bits int
	data []byte
	lock sync.Mutex
}

func (this *BitMap) Set(data []byte) {
	this.data = data
	this.bits = len(data) * 8
}

func (this *BitMap) IsLocked(index int) bool {
	if this.bits > index {
		if (this.data[index/8] & (1 << (uint(7 - (index % 8))))) > 0 {
			return true
		}
	}
	return false
}

func (this *BitMap) LockPiece(index int) bool {
	status := false
	if this.bits > index {
		this.lock.Lock()
		if this.IsLocked(index) == false {
			this.data[index/8] |= (1 << uint(7-(index%8)))
			status = true
		}
		this.lock.Unlock()
	}
	return status
}

func (this *BitMap) UnlockPiece(index int) {
	if index < this.bits {
		this.lock.Lock()
		negation := byte(0xff ^ (1 << uint(7-index%8)))
		this.data[index/8] &= negation
		this.lock.Unlock()
	}
}

func (this *BitMap) GetByte(index int) byte {
	if index < this.bits {
		return this.data[index/8]
	}
	return 0
}
