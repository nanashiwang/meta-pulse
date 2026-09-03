package ledger

import (
	"errors"
	"math"
	"testing"
)

func validEntry(amount int64, key string, after int64) Entry {
	return Entry{UserID: 7, PeriodID: 3, AssetType: AssetContribution, Operation: OperationContributionEarn, Amount: amount, BalanceAfter: after, SourceType: "test", SourceRef: key, IdempotencyKey: key, PayloadHash: "hash-" + key}
}

func TestRebuildAccountFromAppendOnlyEntries(t *testing.T) {
	account, err := Rebuild(Account{UserID: 7, PeriodID: 3, AssetType: AssetContribution}, []Entry{validEntry(1000, "a", 1000), {UserID: 7, PeriodID: 3, AssetType: AssetContribution, Operation: OperationContributionAdjustment, Amount: -200, BalanceAfter: 800, SourceType: "test", SourceRef: "b", IdempotencyKey: "b", PayloadHash: "hash-b", Reason: "correction"}})
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance != 800 || account.Version != 2 {
		t.Fatalf("account = %+v, want balance 800/version 2", account)
	}
}

func TestRebuildRejectsTamperedBalanceAfter(t *testing.T) {
	if _, err := Rebuild(Account{UserID: 7, PeriodID: 3, AssetType: AssetContribution}, []Entry{validEntry(1000, "a", 999)}); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("error = %v, want invalid entry", err)
	}
}

func TestInvalidCrossAssetOperationRejected(t *testing.T) {
	entry := validEntry(1, "a", 1)
	entry.AssetType = AssetTicket
	if err := ValidateEntry(entry); err == nil {
		t.Fatal("cross-asset operation accepted")
	}
}

func TestReversalRequiresReferenceAndCorrectSign(t *testing.T) {
	entry := validEntry(-1, "reverse", -1)
	entry.Operation = OperationContributionReverse
	if err := ValidateEntry(entry); err == nil {
		t.Fatal("unreferenced reversal accepted")
	}
	id := uint64(1)
	entry.ReversalOfEntryID = &id
	if err := ValidateEntry(entry); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRejectsBalanceOverflow(t *testing.T) {
	account := Account{UserID: 7, PeriodID: 3, AssetType: AssetContribution, Balance: math.MaxInt64}
	if _, err := Apply(account, validEntry(1, "overflow", 0)); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("error = %v, want balance overflow", err)
	}
}
