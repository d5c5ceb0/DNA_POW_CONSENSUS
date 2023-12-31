package ledger

import (
	. "DNA_POW/common"
	"DNA_POW/common/serialization"
	"DNA_POW/core/auxpow"
	"DNA_POW/core/contract/program"
	sig "DNA_POW/core/signature"
	. "DNA_POW/errors"
	"crypto/sha256"
	"errors"
	"io"
)

type Blockdata struct {
	Version          uint32
	PrevBlockHash    Uint256
	TransactionsRoot Uint256
	Timestamp        uint32
	Bits             uint32
	Height           uint32
	Nonce            uint32
	AuxPow           auxpow.AuxPow
	Program          *program.Program

	hash Uint256
}

//Serialize the blockheader
func (bd *Blockdata) Serialize(w io.Writer) {
	bd.SerializeUnsigned(w)
	bd.AuxPow.Serialize(w)
	w.Write([]byte{byte(1)})
}

//Serialize the blockheader data without program
func (bd *Blockdata) SerializeUnsigned(w io.Writer) error {
	//REVD: implement blockheader SerializeUnsigned
	serialization.WriteUint32(w, bd.Version)
	bd.PrevBlockHash.Serialize(w)
	bd.TransactionsRoot.Serialize(w)
	serialization.WriteUint32(w, bd.Timestamp)
	serialization.WriteUint32(w, bd.Bits)
	serialization.WriteUint32(w, bd.Nonce)
	serialization.WriteUint32(w, bd.Height)

	return nil
}

func (bd *Blockdata) Deserialize(r io.Reader) error {
	//REVD：Blockdata Deserialize
	bd.DeserializeUnsigned(r)
	bd.AuxPow.Deserialize(r)
	p := make([]byte, 1)
	n, _ := r.Read(p)
	if n > 0 {
		x := []byte(p[:])

		if x[0] != byte(1) {
			return NewDetailErr(errors.New("Blockdata Deserialize get format error."), ErrNoCode, "")
		}
	} else {
		return NewDetailErr(errors.New("Blockdata Deserialize get format error."), ErrNoCode, "")
	}

	return nil
}

func (bd *Blockdata) DeserializeUnsigned(r io.Reader) error {
	//Version
	temp, err := serialization.ReadUint32(r)
	if err != nil {
		return NewDetailErr(err, ErrNoCode, "Blockdata item Version Deserialize failed.")
	}
	bd.Version = temp

	//PrevBlockHash
	preBlock := new(Uint256)
	err = preBlock.Deserialize(r)
	if err != nil {
		return NewDetailErr(err, ErrNoCode, "Blockdata item preBlock Deserialize failed.")
	}
	bd.PrevBlockHash = *preBlock

	//TransactionsRoot
	txRoot := new(Uint256)
	err = txRoot.Deserialize(r)
	if err != nil {
		return err
	}
	bd.TransactionsRoot = *txRoot

	//Timestamp
	temp, _ = serialization.ReadUint32(r)
	bd.Timestamp = uint32(temp)

	//Bits
	temp, _ = serialization.ReadUint32(r)
	bd.Bits = uint32(temp)

	//Nonce
	temp, _ = serialization.ReadUint32(r)
	bd.Nonce = uint32(temp)

	//Height
	temp, _ = serialization.ReadUint32(r)
	bd.Height = uint32(temp)

	return nil
}

func (bd *Blockdata) GetProgramHashes() ([]Uint160, error) {
	return nil, nil
}

func (bd *Blockdata) SetPrograms(programs []*program.Program) {
}

func (bd *Blockdata) GetPrograms() []*program.Program {
	return nil
}

func (bd *Blockdata) Hash() Uint256 {

	d := sig.GetHashData(bd)
	temp := sha256.Sum256([]byte(d))
	f := sha256.Sum256(temp[:])
	hash := Uint256(f)
	return hash
}

func (bd *Blockdata) GetMessage() []byte {
	return sig.GetHashData(bd)
}

func (bd *Blockdata) GetPreBlockHash() Uint256 {
	return bd.PrevBlockHash
}
