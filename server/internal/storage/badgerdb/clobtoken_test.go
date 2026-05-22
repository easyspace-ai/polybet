package badgerdb

import "testing"

func TestCLOBTokenLookupVariants_decimalAndHex(t *testing.T) {
	decimal := "7132104567925221259462638553270691275033272857193029453120401956587936"
	hex := NormalizeCLOBTokenID(decimal)
	variants := CLOBTokenLookupVariants(hex)
	hasDecimal := false
	hasHex := false
	for _, v := range variants {
		if v == decimal {
			hasDecimal = true
		}
		if v == hex {
			hasHex = true
		}
	}
	if !hasHex {
		t.Fatalf("expected hex variant %q in %v", hex, variants)
	}
	if !hasDecimal {
		t.Fatalf("expected decimal variant in %v", variants)
	}
}

func TestCLOBTokenLookupVariants_decimalInput(t *testing.T) {
	decimal := "7132104567925221259462638553270691275033272857193029453120401956587936"
	variants := CLOBTokenLookupVariants(decimal)
	if len(variants) < 2 {
		t.Fatalf("expected at least 2 variants, got %v", variants)
	}
}
