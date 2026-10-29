package server

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/identity"
)

// TestResolveSeedInput_RejectsATypo: a host seed must look like a host name,
// an address or a hash (#159); a typo is refused, not rooted on.
func TestResolveSeedInput_RejectsATypo(t *testing.T) {
	res := identity.NewResolver(uuid.New())
	for _, bad := range []string{"WIN-FILE)1", "-win01", "win01.", "host/name"} {
		if _, err := resolveSeedInput(res, bad, "entity"); err == nil {
			t.Errorf("%q accepted as a host", bad)
		}
	}
	for _, ok := range []string{"WIN-FILE01", "finance-07.contoso.local", "SRV_DB2", "185.220.101.5", "d41d8cd98f00b204e9800998ecf8427e"} {
		if _, err := resolveSeedInput(res, ok, "entity"); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
}

// TestUnknownHost_NoInventory: with no host inventory connected nothing can
// vouch either way, so the seed stands.
func TestUnknownHost_NoInventory(t *testing.T) {
	b := &Backend{}
	res, _ := resolveSeedInput(identity.NewResolver(uuid.New()), "WIN-FILE01", "entity")
	if unknown, _ := b.unknownHost(httptest.NewRequest("POST", "/", nil), res); unknown {
		t.Error("a host reported unknown with no inventory to ask")
	}
}
