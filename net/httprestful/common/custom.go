package common

import (
	. "DNA_POW/common"
	tx "DNA_POW/core/transaction"
	. "DNA_POW/errors"
	. "DNA_POW/net/httpjsonrpc"
	Err "DNA_POW/net/httprestful/error"
	"bytes"
	"encoding/json"
	"time"
)

const AttributeMaxLen = 252

//record
func getRecordData(cmd map[string]interface{}) ([]byte, int64) {
	if raw, ok := cmd["Raw"].(string); ok && raw == "1" {
		str, ok := cmd["RecordData"].(string)
		if !ok {
			return nil, Err.INVALID_PARAMS
		}
		bys, err := HexStringToBytes(str)
		if err != nil {
			return nil, Err.INVALID_PARAMS
		}
		return bys, Err.SUCCESS
	}
	type Data struct {
		Algrithem string `json:Algrithem`
		Hash      string `json:Hash`
		Signature string `json:Signature`
		Text      string `json:Text`
	}
	type RecordData struct {
		CAkey     string  `json:CAkey`
		Data      Data    `json:Data`
		SeqNo     string  `json:SeqNo`
		Timestamp float64 `json:Timestamp`
	}

	tmp := &RecordData{}
	reqRecordData, ok := cmd["RecordData"].(map[string]interface{})
	if !ok {
		return nil, Err.INVALID_PARAMS
	}
	reqBtys, err := json.Marshal(reqRecordData)
	if err != nil {
		return nil, Err.INVALID_PARAMS
	}

	if err := json.Unmarshal(reqBtys, tmp); err != nil {
		return nil, Err.INVALID_PARAMS
	}
	tmp.CAkey, ok = cmd["CAkey"].(string)
	if !ok {
		return nil, Err.INVALID_PARAMS
	}
	repBtys, err := json.Marshal(tmp)
	if err != nil {
		return nil, Err.INVALID_PARAMS
	}
	return repBtys, Err.SUCCESS
}
func getInnerTimestamp() ([]byte, int64) {
	type InnerTimestamp struct {
		InnerTimestamp float64 `json:InnerTimestamp`
	}
	tmp := &InnerTimestamp{InnerTimestamp: float64(time.Now().Unix())}
	repBtys, err := json.Marshal(tmp)
	if err != nil {
		return nil, Err.INVALID_PARAMS
	}
	return repBtys, Err.SUCCESS
}
func SendRecord(cmd map[string]interface{}) map[string]interface{} {
	resp := ResponsePack(Err.SUCCESS)
	var recordData []byte
	var innerTime []byte
	innerTime, resp["Error"] = getInnerTimestamp()
	if innerTime == nil {
		return resp
	}
	recordData, resp["Error"] = getRecordData(cmd)
	if recordData == nil {
		return resp
	}

	var inputs []*tx.UTXOTxInput
	var outputs []*tx.TxOutput

	transferTx, _ := tx.NewTransferAssetTransaction(inputs, outputs)

	rcdInner := tx.NewTxAttribute(tx.Description, innerTime)
	transferTx.Attributes = append(transferTx.Attributes, &rcdInner)

	bytesBuf := bytes.NewBuffer(recordData)

	buf := make([]byte, AttributeMaxLen)
	for {
		n, err := bytesBuf.Read(buf)
		if err != nil {
			break
		}
		var data = make([]byte, n)
		copy(data, buf[0:n])
		record := tx.NewTxAttribute(tx.Description, data)
		transferTx.Attributes = append(transferTx.Attributes, &record)
	}
	if errCode := VerifyAndSendTx(transferTx); errCode != ErrNoError {
		resp["Error"] = int64(errCode)
		return resp
	}
	hash := transferTx.Hash()
	resp["Result"] = BytesToHexString(hash.ToArrayReverse())
	return resp
}

func SendRecordTransaction(cmd map[string]interface{}) map[string]interface{} {
	resp := ResponsePack(Err.SUCCESS)
	var recordData []byte
	recordData, resp["Error"] = getRecordData(cmd)
	if recordData == nil {
		return resp
	}
	recordType := "record"
	recordTx, _ := tx.NewRecordTransaction(recordType, recordData)

	hash := recordTx.Hash()
	resp["Result"] = BytesToHexString(hash.ToArrayReverse())
	if errCode := VerifyAndSendTx(recordTx); errCode != ErrNoError {
		resp["Error"] = int64(errCode)
		return resp
	}
	return resp
}
