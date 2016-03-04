package Torrent

import (
	//"fmt"
	"sync"
)

type BitArray struct {
	bits int
	data []byte
	lock sync.Mutex
}

func (this *BitArray) Set(data []byte) {
	this.data = data
	this.bits = len(data) * 8
}

func (this *BitArray) IsLocked(index int) bool {
	if index < this.bits && index >= 0 {
		if (this.data[index/8] & (1 << (uint(7 - (index % 8))))) > 0 {
			return true
		}
	}
	return false
}

func (this *BitArray) LockPiece(index int) bool {
	status := false
	if index < this.bits && index >= 0 {
		this.lock.Lock()
		if this.IsLocked(index) == false {
			this.data[index/8] |= (1 << uint(7-(index%8)))
			status = true
		}
		this.lock.Unlock()
	}
	return status
}

func (this *BitArray) UnlockPiece(index int) {
	if index < this.bits {
		this.lock.Lock()
		negation := byte(0xff ^ (1 << uint(7-index%8)))
		this.data[index/8] &= negation
		this.lock.Unlock()
	}
}

func (this *BitArray) GetByte(index int) byte {
	if index < this.bits {
		return this.data[index/8]
	}
	return 0
}

func (this *BitArray) GetArray() []byte {
	return this.data
}
