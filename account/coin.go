package account

import (
	"io"

	"DNA_POW/core/transaction"
	"DNA_POW/common/serialization"
)
type AddressType byte

const (
	SingleSign AddressType = 0
	MultiSign  AddressType = 1
)

type Coin struct {
	Output      *transaction.TxOutput
	AddressType AddressType
	Height		uint32
}

func (coin *Coin) Serialize(w io.Writer, version string) error {
	coin.Output.Serialize(w)
	w.Write([]byte{byte(coin.AddressType)})
	serialization.WriteUint32(w, coin.Height)

	return nil
}

func (coin *Coin) Deserialize(r io.Reader, version string) error {
	coin.Output = new(transaction.TxOutput)
	coin.Output.Deserialize(r)
	addrType, err := serialization.ReadUint8(r)
	if err != nil {
		return err
	}
	coin.AddressType = AddressType(addrType)

	height,err := serialization.ReadUint32(r)
	if err != nil {
		return err
	}
	coin.Height = height

	return nil
}
