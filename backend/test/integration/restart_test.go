package integration

import (
	"testing"

	"interstellar-trade-platform/backend/internal/service"
	"interstellar-trade-platform/backend/internal/storage"
)

// Before the fix, saveJSONAtomic (MarshalIndent) re-indented the raw signed
// payloads inside the state file. After a restart the exported envelopes no
// longer matched their signatures and ledger hashes no longer verified.
func TestFileStoreRestartKeepsSignedPayloadsIntact(t *testing.T) {
	skipOnPG(t, "state-file persistence")
	f := newFixture(t)
	f.offer(t, 100)
	if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.earth, UserID: f.seller, Amount: 1}); err != nil {
		t.Fatal(err)
	}

	restarted, err := storage.NewStore(f.statePath) // simulate a process restart
	if err != nil {
		t.Fatal(err)
	}
	f2 := newFixtureFrom(f, service.NewService(restarted))
	b := f2.export(t, f2.earth, f2.mars)
	if _, err := f2.importInto(f2.mars, b); err != nil {
		t.Fatalf("envelope exported after a restart is rejected: %v", err)
	}
	// Read the file exactly as a migration or audit tool would (no JSON round
	// trip that would re-compact the payloads and hide the problem).
	st, err := storage.LoadStateFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.VerifyStateIntegrity(st); err != nil {
		t.Fatalf("ledger integrity broken in the state file: %v", err)
	}
	_ = restarted
}

func newFixtureFrom(f *fixture, svc *service.Service) *fixture {
	g := *f
	g.svc = svc
	return &g
}
