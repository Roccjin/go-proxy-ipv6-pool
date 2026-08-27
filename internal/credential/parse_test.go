package credential

import "testing"

func TestParseRotateBare(t *testing.T) {
	p, err := Parse("caomao002")
	if err != nil {
		t.Fatal(err)
	}
	if p.Account != "caomao002" || p.SID != "" || p.Mode != "" {
		t.Fatalf("%+v", p)
	}
	got := ApplyDefaults(p, ModeRotate, 10, 180)
	if got.Mode != ModeRotate || got.SID != "" || got.TTLMinutes != 10 {
		t.Fatalf("%+v", got)
	}
}

func TestParseSticky(t *testing.T) {
	p, err := Parse("caomao002_sid_46916889_time_10")
	if err != nil {
		t.Fatal(err)
	}
	if p.Account != "caomao002" || p.SID != "46916889" || p.TTLMinutes != 10 {
		t.Fatalf("%+v", p)
	}
	got := ApplyDefaults(p, ModeRotate, 10, 180)
	if got.Mode != ModeSticky {
		t.Fatalf("mode=%s", got.Mode)
	}
}

func TestParseRotateOverridesSID(t *testing.T) {
	p, err := Parse("caomao002_sid_46916889_time_10_mode_rotate")
	if err != nil {
		t.Fatal(err)
	}
	got := ApplyDefaults(p, ModeSticky, 10, 180)
	if got.Mode != ModeRotate || got.SID != "" {
		t.Fatalf("%+v", got)
	}
}

func TestParseGeoIgnored(t *testing.T) {
	p, err := Parse("caomao002_zone_GLOBAL_st_CA_city_LA_sid_46916889_time_10")
	if err != nil {
		t.Fatal(err)
	}
	if p.Account != "caomao002" || p.SID != "46916889" || p.TTLMinutes != 10 {
		t.Fatalf("%+v", p)
	}
}

func TestParseMidCompat(t *testing.T) {
	mid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	p, err := Parse("proxy-mid_" + mid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Account != "proxy" || p.SID != mid || p.Mode != ModeSticky {
		t.Fatalf("%+v", p)
	}
}

func TestClampTTL(t *testing.T) {
	p, _ := Parse("user_sid_1234_time_0")
	got := ApplyDefaults(p, ModeRotate, 10, 180)
	if got.TTLMinutes != 1 {
		t.Fatalf("time=0 -> %d", got.TTLMinutes)
	}
	p, _ = Parse("user_sid_1234_time_999")
	got = ApplyDefaults(p, ModeRotate, 10, 180)
	if got.TTLMinutes != 180 {
		t.Fatalf("time=999 -> %d", got.TTLMinutes)
	}
}

func TestInvalidAccountReserved(t *testing.T) {
	if err := ValidAccountName("foo_sid_bar"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatUsername(t *testing.T) {
	if g := FormatUsername("a", "12345678", 10, ModeSticky); g != "a_sid_12345678_time_10" {
		t.Fatal(g)
	}
	if g := FormatUsername("a", "12345678", 10, ModeRotate); g != "a" {
		t.Fatal(g)
	}
}

func TestRandomSID(t *testing.T) {
	s, err := RandomSID()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidSID(s) || len(s) != 8 {
		t.Fatalf("%q", s)
	}
}
