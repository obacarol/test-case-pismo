package money

import "testing"

func TestParsePositiveDecimal(t *testing.T) {
	t.Parallel()

	ok := []struct {
		in   string
		want Cents
	}{
		{"123.45", 12345},
		{"123.4", 12340},
		{"123", 12300},
		{"0.01", 1},
		{"0.10", 10},
		{"50.0", 5000},
		{"00.50", 50},
		{"92233720368547758.07", 9223372036854775807},
	}
	for _, tc := range ok {
		got, err := ParsePositiveDecimal(tc.in)
		if err != nil {
			t.Fatalf("ParsePositiveDecimal(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParsePositiveDecimal(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}

	bad := []string{
		"", "0", "0.00", "-1", "-1.00", "+10", "1.234", "1e2", "1E2",
		"10.", ".5", "1.2.3", " 1", "1 ", "abc", "50.100",
		"92233720368547758.08",
	}
	for _, in := range bad {
		if _, err := ParsePositiveDecimal(in); err == nil {
			t.Fatalf("ParsePositiveDecimal(%q) accepted an invalid amount", in)
		}
	}
}

func TestCentsJSON(t *testing.T) {
	t.Parallel()

	var c Cents
	if err := c.UnmarshalJSON([]byte("23.5")); err != nil {
		t.Fatal(err)
	}
	if c != 2350 {
		t.Fatalf("got %d cents", c)
	}
	out, err := c.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "23.50" {
		t.Fatalf("marshal = %s", out)
	}

	neg := Cents(-5000)
	out, err = neg.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "-50.00" {
		t.Fatalf("marshal negative = %s", out)
	}

	if err := c.UnmarshalJSON([]byte(`"10.00"`)); err == nil {
		t.Fatal("string amount was accepted")
	}
	if err := c.UnmarshalJSON([]byte("")); err == nil {
		t.Fatal("empty amount was accepted")
	}
	if err := c.UnmarshalJSON([]byte("   ")); err == nil {
		t.Fatal("blank amount was accepted")
	}
}

func TestFormatRejectsMinInt64(t *testing.T) {
	t.Parallel()
	if _, err := Cents(-9223372036854775808).Format(); err == nil {
		t.Fatal("MinInt64 should not format")
	}
	if _, err := Cents(-9223372036854775808).MarshalJSON(); err == nil {
		t.Fatal("MinInt64 should not marshal")
	}
}
