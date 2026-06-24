package apollo

import (
	"math/big"
	"testing"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	plutigoData "github.com/blinklabs-io/plutigo/data"

	"github.com/Salvionied/apollo/v2/backend"
	"github.com/Salvionied/apollo/v2/backend/fixed"
)

// TestComputeScriptDataHashOmitsEmptyDatums locks the fix for the
// ScriptIntegrityHashMismatch that the Cardano node raised on datum-less
// script transactions (e.g. reward-withdrawal redeemers). The ledger
// (Conway UtxoValidateScriptDataHash) concatenates redeemers || datums ||
// langViews and appends NOTHING for datums when the witness set has none.
// Encoding an empty array (0x80) would shift the hash by one byte. This test
// reconstructs the ledger's exact hash input and asserts ComputeScriptDataHash
// matches it byte-for-byte.
func TestComputeScriptDataHashOmitsEmptyDatums(t *testing.T) {
	redeemers := map[common.RedeemerKey]common.RedeemerValue{
		{Tag: common.RedeemerTagReward, Index: 0}: {
			Data:    common.Datum{Data: plutigoData.NewInteger(big.NewInt(42))},
			ExUnits: common.ExUnits{Memory: 1000, Steps: 2000},
		},
	}
	v3 := []int64{100788, 420, 1, 1, 1000, 173, 0, 1}
	costModels := map[string][]int64{"PlutusV3": v3}

	got, err := ComputeScriptDataHash(redeemers, nil, costModels)
	if err != nil {
		t.Fatalf("ComputeScriptDataHash: %v", err)
	}
	if got == nil {
		t.Fatal("expected a hash")
	}

	// Reconstruct exactly as the ledger does: redeemers || langViews, with no
	// datum bytes at all when there are no datums.
	redeemerCbor, err := cbor.Encode(redeemers)
	if err != nil {
		t.Fatal(err)
	}
	langViews, err := common.EncodeLangViews(
		map[uint]struct{}{2: {}},
		map[uint][]int64{2: v3},
	)
	if err != nil {
		t.Fatal(err)
	}
	expectedInput := make([]byte, 0, len(redeemerCbor)+len(langViews))
	expectedInput = append(expectedInput, redeemerCbor...)
	expectedInput = append(expectedInput, langViews...)
	want := common.Blake2b256Hash(expectedInput)
	if *got != want {
		t.Fatalf("script data hash mismatch with ledger formula:\n got  %x\n want %x", got.Bytes(), want.Bytes())
	}

	// Guard against a regression to the old behaviour: appending an empty-array
	// (0x80) datum must NOT produce the same hash.
	wrongInput := make([]byte, 0, len(redeemerCbor)+1+len(langViews))
	wrongInput = append(wrongInput, redeemerCbor...)
	wrongInput = append(wrongInput, 0x80)
	wrongInput = append(wrongInput, langViews...)
	wrong := common.Blake2b256Hash(wrongInput)
	if *got == wrong {
		t.Fatal("ComputeScriptDataHash still encodes an empty-array datum (0x80); this reintroduces the ScriptIntegrityHashMismatch")
	}
}

// TestTierRefScriptFee locks the Conway tiered reference-script fee, whose
// omission caused FeeTooSmallUTxO. It checks the flat single-tier case, the
// multi-tier growth, and the base-price fallbacks.
func TestTierRefScriptFee(t *testing.T) {
	// Single tier (size below the increment): floor(size * base).
	if got := backend.TierRefScriptFee(13892, 15, 25600, 1.2); got != 13892*15 {
		t.Fatalf("single-tier fee = %d, want %d", got, 13892*15)
	}
	// Zero base price => zero fee (pre-Conway / provider without ref pricing).
	if got := backend.TierRefScriptFee(13892, 0, 25600, 1.2); got != 0 {
		t.Fatalf("zero-base fee = %d, want 0", got)
	}
	// Zero size => zero fee.
	if got := backend.TierRefScriptFee(0, 15, 25600, 1.2); got != 0 {
		t.Fatalf("zero-size fee = %d, want 0", got)
	}
	// Multi-tier: first 25600 bytes at 15, next 4400 at 15*1.2=18.
	// floor(25600*15 + 4400*18) = floor(384000 + 79200) = 463200.
	if got := backend.TierRefScriptFee(30000, 15, 25600, 1.2); got != 463200 {
		t.Fatalf("multi-tier fee = %d, want %d", got, 463200)
	}

	// Protocol-parameter accessors fall back to ledger constants and prefer the
	// structured base when present.
	ppFlat := backend.ProtocolParameters{MinFeeRefScriptCostPerByte: 15}
	if ppFlat.RefScriptFeePerByte() != 15 {
		t.Fatalf("flat RefScriptFeePerByte = %v, want 15", ppFlat.RefScriptFeePerByte())
	}
	if ppFlat.RefScriptSizeIncrement() != backend.DefaultRefScriptSizeIncrement {
		t.Fatalf("default size increment = %d, want %d", ppFlat.RefScriptSizeIncrement(), backend.DefaultRefScriptSizeIncrement)
	}
	if ppFlat.RefScriptMultiplier() != backend.DefaultRefScriptMultiplier {
		t.Fatalf("default multiplier = %v, want %v", ppFlat.RefScriptMultiplier(), backend.DefaultRefScriptMultiplier)
	}
	ppStruct := backend.ProtocolParameters{
		MinFeeReferenceScriptsBase:       44,
		MinFeeReferenceScriptsRange:      30000,
		MinFeeReferenceScriptsMultiplier: 2,
	}
	if ppStruct.RefScriptFeePerByte() != 44 {
		t.Fatalf("structured RefScriptFeePerByte = %v, want 44", ppStruct.RefScriptFeePerByte())
	}
	if ppStruct.RefScriptSizeIncrement() != 30000 {
		t.Fatalf("structured size increment = %d, want 30000", ppStruct.RefScriptSizeIncrement())
	}
	if ppStruct.RefScriptMultiplier() != 2 {
		t.Fatalf("structured multiplier = %v, want 2", ppStruct.RefScriptMultiplier())
	}
}

// TestFinalizeCollateralUsesFinalFee locks the InsufficientCollateral fix:
// total collateral and the collateral return must be sized from the FINAL fee
// (ceil(fee * collateralPercent / 100)), not the preliminary max-by-size fee
// that setCollateral() had available.
func TestFinalizeCollateralUsesFinalFee(t *testing.T) {
	pp := backend.ProtocolParameters{
		MinFeeConstant:    155381,
		MinFeeCoefficient: 44,
		MaxTxSize:         16384,
		CoinsPerUtxoByte:  "4310",
		CollateralPercent: 150,
	}
	gp := backend.GenesisParameters{NetworkMagic: 1}
	cc := fixed.NewFixedChainContext(pp, gp, 0)
	addr := testAddress(t)

	var collHash common.Blake2b256
	collHash[0] = 0x42
	collateralUtxo := makeTestUtxo(t, collHash, 0, 5_000_000)

	a := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AttachScript(common.PlutusV2Script([]byte{0x01, 0x02})).
		AddLoadedUTxOs(collateralUtxo)

	if err := a.setCollateral(); err != nil {
		t.Fatalf("setCollateral: %v", err)
	}
	// setCollateral sizes from the max-by-size fee (16384*44+155381 = 876277):
	// ceil-ish 876277*150/100 = 1314415. Confirm it is NOT yet the final value.
	prelim := a.totalCollateral
	if prelim == 0 {
		t.Fatal("expected setCollateral to record a preliminary total collateral")
	}

	const finalFee = 1_255_870
	if err := a.finalizeCollateral(finalFee); err != nil {
		t.Fatalf("finalizeCollateral: %v", err)
	}

	wantRequired := int64((finalFee*150 + 99) / 100) // ceil(fee*percent/100)
	if a.totalCollateral != wantRequired {
		t.Fatalf("total collateral = %d, want %d (ceil(%d*150/100))", a.totalCollateral, wantRequired, finalFee)
	}
	if wantRequired <= prelim {
		t.Fatalf("test fixture invalid: final required %d should exceed preliminary %d", wantRequired, prelim)
	}
	// Collateral return must carry the remainder forward.
	if a.collateralReturn == nil {
		t.Fatal("expected a collateral return output")
	}
	wantRemainder := uint64(5_000_000 - wantRequired)
	if a.collateralReturn.OutputAmount.Amount != wantRemainder {
		t.Fatalf("collateral return = %d, want %d", a.collateralReturn.OutputAmount.Amount, wantRemainder)
	}
}

// TestFinalizeCollateralRespectsExplicitAmount ensures a user-pinned collateral
// amount is left untouched by the fee-driven resize.
func TestFinalizeCollateralRespectsExplicitAmount(t *testing.T) {
	pp := backend.ProtocolParameters{
		MinFeeConstant:    155381,
		MinFeeCoefficient: 44,
		MaxTxSize:         16384,
		CoinsPerUtxoByte:  "4310",
		CollateralPercent: 150,
	}
	gp := backend.GenesisParameters{NetworkMagic: 1}
	cc := fixed.NewFixedChainContext(pp, gp, 0)
	addr := testAddress(t)

	var collHash common.Blake2b256
	collHash[0] = 0x43
	collateralUtxo := makeTestUtxo(t, collHash, 0, 5_000_000)

	a := New(cc).
		SetWallet(NewExternalWallet(addr)).
		SetCollateralAmount(5_000_000).
		AttachScript(common.PlutusV2Script([]byte{0x01, 0x02})).
		AddLoadedUTxOs(collateralUtxo)

	if err := a.setCollateral(); err != nil {
		t.Fatalf("setCollateral: %v", err)
	}
	before := a.totalCollateral
	if err := a.finalizeCollateral(1_255_870); err != nil {
		t.Fatalf("finalizeCollateral: %v", err)
	}
	if a.totalCollateral != before {
		t.Fatalf("explicit collateral amount was resized: before %d after %d", before, a.totalCollateral)
	}
}
