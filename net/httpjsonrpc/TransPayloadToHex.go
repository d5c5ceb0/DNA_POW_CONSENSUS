package httpjsonrpc

import (
	"bytes"

	. "DNA_POW/common"
	"DNA_POW/core/asset"
	. "DNA_POW/core/contract"
	. "DNA_POW/core/transaction"
	"DNA_POW/core/transaction/payload"
)

type PayloadInfo interface{}

//implement PayloadInfo define BookKeepingInfo
type BookKeepingInfo struct {
	Nonce  uint64
	Issuer IssuerInfo
}

//implement PayloadInfo define DeployCodeInfo
type FunctionCodeInfo struct {
	Code           string
	ParameterTypes string
	ReturnTypes    string
}

type DeployCodeInfo struct {
	Code        *FunctionCodeInfo
	Name        string
	CodeVersion string
	Author      string
	Email       string
	Description string
}

type IssuerInfo struct {
	X, Y string
}

//implement PayloadInfo define RegisterAssetInfo
type RegisterAssetInfo struct {
	Asset      *asset.Asset
	Amount     float64
	Issuer     IssuerInfo
	Controller string
}

type RecordInfo struct {
	RecordType string
	RecordData string
}

type BookkeeperInfo struct {
	PubKey     string
	Action     string
	Issuer     IssuerInfo
	Controller string
}

type DataFileInfo struct {
	IPFSPath string
	Filename string
	Note     string
	Issuer   IssuerInfo
}

type PrivacyPayloadInfo struct {
	PayloadType uint8
	Payload     string
	EncryptType uint8
	EncryptAttr string
}

func TransPayloadToHex(p Payload) PayloadInfo {
	switch object := p.(type) {
	case *payload.BookKeeping:
		obj := new(BookKeepingInfo)
		obj.Nonce = object.Nonce
		return obj
	case *payload.BookKeeper:
		obj := new(BookkeeperInfo)
		encodedPubKey, _ := object.PubKey.EncodePoint(true)
		obj.PubKey = ToHexString(encodedPubKey)
		if object.Action == payload.BookKeeperAction_ADD {
			obj.Action = "add"
		} else if object.Action == payload.BookKeeperAction_SUB {
			obj.Action = "sub"
		} else {
			obj.Action = "nil"
		}
		obj.Issuer.X = object.Issuer.X.String()
		obj.Issuer.Y = object.Issuer.Y.String()

		return obj
	case *payload.IssueAsset:
	case *payload.TransferAsset:
	case *payload.DeployCode:
		obj := new(DeployCodeInfo)
		obj.Code.Code = ToHexString(object.Code.Code)
		obj.Code.ParameterTypes = ToHexString(ContractParameterTypeToByte(object.Code.ParameterTypes))
		obj.Code.ReturnTypes = ToHexString(ContractParameterTypeToByte(object.Code.ReturnTypes))
		obj.Name = object.Name
		obj.CodeVersion = object.CodeVersion
		obj.Author = object.Author
		obj.Email = object.Email
		obj.Description = object.Description
		return obj
	case *payload.RegisterAsset:
		obj := new(RegisterAssetInfo)
		obj.Asset = object.Asset
		obj.Amount = float64(object.Amount) / 100000000
		obj.Issuer.X = object.Issuer.X.String()
		obj.Issuer.Y = object.Issuer.Y.String()
		obj.Controller = ToHexString(object.Controller.ToArray())
		return obj
	case *payload.Record:
		obj := new(RecordInfo)
		obj.RecordType = object.RecordType
		obj.RecordData = ToHexString(object.RecordData)
		return obj
	case *payload.PrivacyPayload:
		obj := new(PrivacyPayloadInfo)
		obj.PayloadType = uint8(object.PayloadType)
		obj.Payload = ToHexString(object.Payload)
		obj.EncryptType = uint8(object.EncryptType)
		bytesBuffer := bytes.NewBuffer([]byte{})
		object.EncryptAttr.Serialize(bytesBuffer)
		obj.EncryptAttr = ToHexString(bytesBuffer.Bytes())
		return obj
	case *payload.DataFile:
		obj := new(DataFileInfo)
		obj.IPFSPath = object.IPFSPath
		obj.Filename = object.Filename
		obj.Note = object.Note
		obj.Issuer.X = object.Issuer.X.String()
		obj.Issuer.Y = object.Issuer.Y.String()
		return obj
	}
	return nil
}
