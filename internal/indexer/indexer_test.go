package indexer

import (
	"sort"
	"testing"

	"github.com/yu-org/explore/internal/model"
)

func TestAddressRefs(t *testing.T) {
	tx := &model.Tx{
		Hash:   "0xtx",
		Height: 7,
		Caller: "0x7EAE9c835a9368971f313f9f9f0f98d26f056b36",
		Params: `{"to":"0x45392402C6810F8CB00bf70f0d13e32983139A7A","amount":234}`,
		Events: []string{"paid 0xBBea54565D15e21900f3b13f82242637f0cd5750"},
	}

	refs := addressRefs(tx)
	sort.Slice(refs, func(i, j int) bool { return refs[i].Address < refs[j].Address })

	want := []model.AddressRef{
		{Address: "0x45392402c6810f8cb00bf70f0d13e32983139a7a", TxHash: "0xtx", Height: 7, Role: model.RoleParam},
		{Address: "0x7eae9c835a9368971f313f9f9f0f98d26f056b36", TxHash: "0xtx", Height: 7, Role: model.RoleCaller},
		{Address: "0xbbea54565d15e21900f3b13f82242637f0cd5750", TxHash: "0xtx", Height: 7, Role: model.RoleEvent},
	}
	if len(refs) != len(want) {
		t.Fatalf("got %d refs, want %d: %+v", len(refs), len(want), refs)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Errorf("ref %d = %+v, want %+v", i, refs[i], want[i])
		}
	}
}

// The caller role must survive an address that also appears in the params —
// otherwise a self-transfer would demote the caller to a mere mention.
func TestAddressRefsKeepsCallerRole(t *testing.T) {
	addr := "0x7eae9c835a9368971f313f9f9f0f98d26f056b36"
	tx := &model.Tx{Hash: "0xtx", Caller: addr, Params: `{"to":"` + addr + `"}`}

	refs := addressRefs(tx)
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1: %+v", len(refs), refs)
	}
	if refs[0].Role != model.RoleCaller {
		t.Errorf("role = %q, want %q", refs[0].Role, model.RoleCaller)
	}
}

func TestAddressRefsSkipsZeroAddress(t *testing.T) {
	tx := &model.Tx{Hash: "0xtx", Params: `{"to":"` + emptyAddress + `"}`}
	if refs := addressRefs(tx); len(refs) != 0 {
		t.Errorf("got %+v, want no refs", refs)
	}
}
