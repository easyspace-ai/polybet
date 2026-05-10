package routercanon

import "testing"

func TestCanonicalize12WinsSuffix(t *testing.T) {
	home, away := "Lakers", "Celtics"
	r := Canonicalize("Lakers wins", "12", home, away)
	if r.Parts == nil || r.Parts.Side != "home" {
		t.Fatalf("home wins: %+v", r)
	}
	r2 := Canonicalize("Celtics Wins", "12", home, away)
	if r2.Parts == nil || r2.Parts.Side != "away" {
		t.Fatalf("away wins: %+v", r2)
	}
}
