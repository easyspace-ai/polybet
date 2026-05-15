package polyexec

import (
	"strings"
	"testing"
)

func TestCLOBAssetIDForAPI(t *testing.T) {
	hexID := "0x2845b5a7" + strings.Repeat("0", 56)
	if len(hexID) != 66 {
		t.Fatalf("bad test hex len %d", len(hexID))
	}
	dec := CLOBAssetIDForAPI(hexID)
	if dec == "" || dec == hexID {
		t.Fatalf("expected decimal conversion, got %q", dec)
	}
	dec2 := CLOBAssetIDForAPI(dec)
	if dec2 != dec {
		t.Fatalf("decimal passthrough: %q vs %q", dec, dec2)
	}
	if CLOBAssetIDForAPI("") != "" {
		t.Fatal("empty")
	}
	if CLOBAssetIDForAPI("0xzzz") != "" {
		t.Fatal("invalid hex")
	}
}
