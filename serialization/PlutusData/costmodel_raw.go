package PlutusData

import "github.com/fxamacker/cbor/v2"

// This file adds helpers to build language views (cost models) for the Conway
// script_data_hash from the *network* cost-model values supplied by the chain
// context (ProtocolParameters.CostModelsRaw), rather than from the hardcoded
// PLUTUSV1/V2/V3COSTMODEL constants. The hardcoded constants go stale whenever
// the protocol updates its cost models, which makes the on-chain
// script_data_hash mismatch (ConwayUtxowFailure PPViewHashesDontMatch).
//
// CostModelsRaw values are already in canonical ledger order, so they map
// directly onto apollo's existing encodings: V2/V3 as a plain definite array,
// V1 with the special bytestring-wrapped indefinite-array encoding.

// NewCostModelArray builds the plain-array cost model (PlutusV2/V3 encoding)
// from raw int64 values in canonical order.
func NewCostModelArray(raw []int64) CostModelArray {
	out := make(CostModelArray, len(raw))
	for i, v := range raw {
		out[i] = int32(v)
	}
	return out
}

// CostModelV1Raw encodes PlutusV1 cost models from raw values in canonical
// order, mirroring CM.MarshalCBOR's special encoding: the integer array is
// serialized as an indefinite-length array and then wrapped in a CBOR
// bytestring. (CM.MarshalCBOR derives the order by sorting parameter names; the
// raw values are already in that order, so the resulting bytes are identical.)
type CostModelV1Raw []int64

func (c CostModelV1Raw) MarshalCBOR() ([]byte, error) {
	res := make([]int, len(c))
	for i, v := range c {
		res[i] = int(v)
	}
	partial, err := cbor.Marshal(res)
	if err != nil {
		return nil, err
	}
	partial[1] = 0x9f
	partial = append(partial, 0xff)
	return cbor.Marshal(partial[1:])
}
