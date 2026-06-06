package transaction

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// golden values produced by the reference Python XClientTransaction library
// (iSarabjitDhiman/XClientTransaction) on the same fixtures.
const (
	goldenAnimationKey = "7ad100ccccccccccccd00ccccccccccccd100"
	goldenRowIndex     = 2
)

func loadFixtures(t *testing.T) (home, ondemand string) {
	t.Helper()
	h, err := os.ReadFile("testdata/home.html")
	if err != nil {
		t.Fatalf("read home fixture: %v", err)
	}
	o, err := os.ReadFile("testdata/ondemand.js")
	if err != nil {
		t.Fatalf("read ondemand fixture: %v", err)
	}
	return string(h), string(o)
}

func TestParityWithPython(t *testing.T) {
	home, ondemand := loadFixtures(t)
	g, err := New(home, ondemand)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.rowIndex != goldenRowIndex {
		t.Errorf("rowIndex = %d, want %d", g.rowIndex, goldenRowIndex)
	}
	if g.AnimationKey() != goldenAnimationKey {
		t.Errorf("animationKey = %q, want %q", g.AnimationKey(), goldenAnimationKey)
	}
}

func TestGenerateTransactionIDShape(t *testing.T) {
	home, ondemand := loadFixtures(t)
	g, err := New(home, ondemand)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := g.GenerateTransactionID("GET", "/i/api/graphql/abc/UserByScreenName")
	if id == "" {
		t.Fatal("empty transaction id")
	}
	if strings.HasSuffix(id, "=") {
		t.Errorf("transaction id should be unpadded: %q", id)
	}
	// must decode as standard base64 once padding is restored
	padded := id
	if m := len(padded) % 4; m != 0 {
		padded += strings.Repeat("=", 4-m)
	}
	if _, err := base64.StdEncoding.DecodeString(padded); err != nil {
		t.Errorf("transaction id is not valid base64: %v", err)
	}
	// two successive ids should differ (random byte + timestamp)
	if id2 := g.GenerateTransactionID("GET", "/i/api/graphql/abc/UserByScreenName"); id2 == id {
		t.Error("expected successive transaction ids to differ")
	}
}

func TestFloatToHex(t *testing.T) {
	// spot-checks mirroring the Python implementation's behaviour.
	cases := map[float64]string{
		0:   "",
		1:   "1",
		255: "FF",
		16:  "10",
	}
	for in, want := range cases {
		if got := floatToHex(in); got != want {
			t.Errorf("floatToHex(%v) = %q, want %q", in, got, want)
		}
	}
}
