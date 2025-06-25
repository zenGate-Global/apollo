package Utils

import (
	"encoding/hex"
	"math"

	"github.com/Salvionied/apollo/serialization"
	"github.com/Salvionied/apollo/serialization/TransactionInput"
	"github.com/Salvionied/apollo/serialization/TransactionOutput"
	"github.com/Salvionied/apollo/serialization/UTxO"
	"github.com/Salvionied/apollo/txBuilding/Backend/Base"

	"github.com/fxamacker/cbor/v2"
)

func Contains[T UTxO.Container[any]](container []T, contained T) bool {
	for _, c := range container {
		if c.EqualTo(contained) {
			return true
		}
	}
	return false
}

func MinLovelacePostAlonzo(output TransactionOutput.TransactionOutput, context Base.ChainContext) (int64, error) {
	constantOverhead := 200
	amt := output.GetValue()
	if amt.Coin == 0 {
		amt.Coin = 1_000_000
	}
	tmp_out := TransactionOutput.TransactionOutput{
		IsPostAlonzo: true,
		PostAlonzo: TransactionOutput.TransactionOutputAlonzo{
			Address:   output.GetAddress(),
			Amount:    output.GetValue().ToAlonzoValue(),
			Datum:     output.GetDatumOption(),
			ScriptRef: output.GetScriptRef(),
		},
	}
	encoded, err := cbor.Marshal(tmp_out)
	if err != nil {
		return 0, err
	}
	pps, err := context.GetProtocolParams()
	if err != nil {
		return 0, err
	}
	return int64((constantOverhead + len(encoded)) * pps.GetCoinsPerUtxoByte()), nil
}

func ToCbor(x interface{}) (string, error) {
	bytes, err := cbor.Marshal(x)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func Fee(context Base.ChainContext, txSize int, steps int64, mem int64, refInputs []TransactionInput.TransactionInput) (int64, error) {
	pps, err := context.GetProtocolParams()
	if err != nil {
		return 0, err
	}
	addedFee := 0
	refInputsSize := 0
	if len(refInputs) > 0 {
		// APPLY CONWAY FEE
		for _, refInput := range refInputs {
			utxo, err := context.GetUtxoFromRef(hex.EncodeToString(refInput.TransactionId), refInput.Index)
			if err != nil {
				continue
			}
			if utxo == nil {
				continue
			}
			if utxo.Output.GetScriptRef() == nil {
				continue
			}
			refInputsSize += utxo.Output.GetScriptRef().Len()
		}

	}
	mult := 1.2
	baseRefFee := 15.0
	Range := 25600.0
	tempRefInputsSize := float64(refInputsSize)

	for tempRefInputsSize > 0 {
		cur := math.Min(Range, tempRefInputsSize)
		curFee := cur * baseRefFee
		addedFee += int(curFee)
		tempRefInputsSize -= cur
		baseRefFee = baseRefFee * mult
	}

	scriptExecutionFee := math.Ceil((float64(mem) * float64(pps.PriceMem)) + (float64(steps) * float64(pps.PriceStep)))

	totalFee := int64(txSize*pps.MinFeeCoefficient) +
		int64(pps.MinFeeConstant) +
		int64(scriptExecutionFee) +
		int64(addedFee)

	return totalFee, nil
}

func Copy[T serialization.Clonable[T]](input []T) []T {
	res := make([]T, 0)
	for _, value := range input {
		res = append(res, value.Clone())
	}
	return res
}
