package apollo

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/Salvionied/apollo/serialization"
	"github.com/Salvionied/apollo/serialization/PlutusData"
	"github.com/fxamacker/cbor/v2"
)

// sdhFixture mirrors the JSON dumped by dumpScriptDataHash from a real mainnet
// emission tx that the node rejected with PPViewHashesDontMatch.
type sdhFixture struct {
	RedeemerBytesHex  string             `json:"redeemer_bytes_hex"`
	DatumBytesHex     string             `json:"datum_bytes_hex"`
	CostModelBytesHex string             `json:"cost_model_bytes_hex"`
	ScriptDataHashHex string             `json:"script_data_hash_hex"`
	CostModelsRaw     map[string][]int64 `json:"cost_models_raw"`
	ReferenceInputs   int                `json:"reference_inputs"`
}

// The hash the node computed from the *current* mainnet cost models (the
// "expected" SafeHash in the ConwayUtxowFailure PPViewHashesDontMatch error).
const expectedNodeScriptDataHash = "a48ad51eccebc251615e8c69b834750146d4cf166f2f44c1dddc9b38d294941b"

func loadSDHFixture(t *testing.T) sdhFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/sdh_repro.json")
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	var f sdhFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return f
}

func hashSDH(t *testing.T, redeemerHex, datumHex string, costModelBytes []byte) string {
	t.Helper()
	redeemer, err := hex.DecodeString(redeemerHex)
	if err != nil {
		t.Fatal(err)
	}
	datum, err := hex.DecodeString(datumHex)
	if err != nil {
		t.Fatal(err)
	}
	total := append([]byte{}, redeemer...)
	total = append(total, datum...)
	total = append(total, costModelBytes...)
	h, err := serialization.Blake2bHash(total)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h)
}

// TestSDH_HardcodedReproducesRejectedHash is a sanity check: feeding the
// hardcoded PLUTUSV3COSTMODEL through the same encoding path reproduces the
// exact script_data_hash the fork produced (and the node rejected). This proves
// the test harness faithfully mirrors apollo's scriptDataHash().
func TestSDH_HardcodedReproducesRejectedHash(t *testing.T) {
	f := loadSDHFixture(t)
	if f.ReferenceInputs == 0 {
		t.Fatalf("fixture expected reference inputs (V3 path), got 0")
	}
	usedCms := map[any]cbor.Marshaler{2: PlutusData.PLUTUSV3COSTMODEL}
	cmBytes, err := cbor.Marshal(usedCms)
	if err != nil {
		t.Fatal(err)
	}
	got := hashSDH(t, f.RedeemerBytesHex, f.DatumBytesHex, cmBytes)
	if got != f.ScriptDataHashHex {
		t.Fatalf("harness mismatch:\n got  %s\n want %s (fork-produced)", got, f.ScriptDataHashHex)
	}
	t.Logf("hardcoded path reproduces fork hash %s", got)
}

// TestSDH_NetworkCostModelsMatchNode verifies that building the V3 cost-model
// CBOR from the live mainnet CostModelsRaw (instead of the stale hardcoded
// constant) yields the script_data_hash the node expects. This is the target
// behaviour for the fix.
func TestSDH_NetworkCostModelsMatchNode(t *testing.T) {
	f := loadSDHFixture(t)
	v3 := f.CostModelsRaw["PlutusV3"]
	if len(v3) == 0 {
		t.Fatal("fixture missing CostModelsRaw[PlutusV3]")
	}
	usedCms := map[any]cbor.Marshaler{2: PlutusData.NewCostModelArray(v3)}
	cmBytes, err := cbor.Marshal(usedCms)
	if err != nil {
		t.Fatal(err)
	}
	got := hashSDH(t, f.RedeemerBytesHex, f.DatumBytesHex, cmBytes)
	if got != expectedNodeScriptDataHash {
		t.Fatalf("network cost-model hash mismatch:\n got  %s\n want %s (node-expected)", got, expectedNodeScriptDataHash)
	}
	t.Logf("network cost-model path matches node-expected hash %s", got)
}
