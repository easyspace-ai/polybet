package balancesvc

import "testing"

func TestParseCollateralMicro(t *testing.T) {
	v, err := ParseCollateralMicro("5705489")
	if err != nil || v < 5.705 || v > 5.706 {
		t.Fatalf("micro parse: %v %v", v, err)
	}
	v2, err := ParseCollateralMicro("12.5")
	if err != nil || v2 != 12.5 {
		t.Fatalf("float parse: %v %v", v2, err)
	}
}
