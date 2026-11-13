package apollo

import (
	"testing"
)

// TestCompleteAbsorbedChangeIsChargedAsFee reproduces the mainnet
// ValueNotConservedUTxO failure: the only wallet UTxO leaves ADA-only change
// below min-UTxO, so no change output is created. The dropped change must be
// charged as fee, otherwise inputs exceed outputs + fee and the ledger rejects
// the transaction.
func TestCompleteAbsorbedChangeIsChargedAsFee(t *testing.T) {
	const inputLovelace = 3_000_000
	const paymentLovelace = 2_000_000

	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, inputLovelace, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, paymentLovelace, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000).
		Complete()
	if err != nil {
		t.Fatal(err)
	}

	tx := a.GetTx()
	if len(tx.Body.TxInputs.Items()) != 1 {
		t.Fatalf("expected 1 input, got %d", len(tx.Body.TxInputs.Items()))
	}
	if len(tx.Body.TxOutputs) != 1 {
		t.Fatalf("expected the sub-min-UTxO change to be dropped (1 output), got %d outputs", len(tx.Body.TxOutputs))
	}

	var totalOut uint64
	for _, out := range tx.Body.TxOutputs {
		totalOut += out.OutputAmount.Amount
	}
	if got := totalOut + tx.Body.TxFee; got != inputLovelace {
		t.Fatalf("value not conserved: inputs %d, outputs %d + fee %d = %d",
			inputLovelace, totalOut, tx.Body.TxFee, got)
	}
}

// TestCompleteChangeAboveMinIsStillReturned guards the normal path: change
// that clears min-UTxO is returned to the change address, not charged as fee.
func TestCompleteChangeAboveMinIsStillReturned(t *testing.T) {
	const inputLovelace = 10_000_000
	const paymentLovelace = 2_000_000

	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, inputLovelace, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, paymentLovelace, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000).
		Complete()
	if err != nil {
		t.Fatal(err)
	}

	tx := a.GetTx()
	if len(tx.Body.TxOutputs) != 2 {
		t.Fatalf("expected payment + change outputs, got %d", len(tx.Body.TxOutputs))
	}
	var totalOut uint64
	for _, out := range tx.Body.TxOutputs {
		totalOut += out.OutputAmount.Amount
	}
	if got := totalOut + tx.Body.TxFee; got != inputLovelace {
		t.Fatalf("value not conserved: outputs %d + fee %d = %d, want %d",
			totalOut, tx.Body.TxFee, got, inputLovelace)
	}
	if tx.Body.TxFee > 1_000_000 {
		t.Fatalf("fee %d unexpectedly large; change should not be absorbed", tx.Body.TxFee)
	}
}
