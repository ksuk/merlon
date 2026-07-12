package logging

import "testing"

func TestMaskCustomerName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"japanese name", "田中太郎", "田***"},
		{"ascii name", "Alice", "A***"},
		{"empty", "", "***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskCustomerName(tc.in); got != tc.want {
				t.Errorf("MaskCustomerName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMaskEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"standard", "keisuke@example.com", "ke***@example.com"},
		{"short local part", "a@example.com", "a***@example.com"},
		{"no at sign", "notanemail", "***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskEmail(tc.in); got != tc.want {
				t.Errorf("MaskEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMaskPhone(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"hyphenated mobile", "090-1234-5678", "***-***-5678"},
		{"hyphenated landline", "03-1234-5678", "***-***-5678"},
		{"digits only", "09012345678", "***5678"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskPhone(tc.in); got != tc.want {
				t.Errorf("MaskPhone(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMaskExternalID(t *testing.T) {
	got := MaskExternalID("cust-00123")
	if len(got) != 8 {
		t.Errorf("MaskExternalID length = %d, want 8", len(got))
	}
	if got2 := MaskExternalID("cust-00123"); got2 != got {
		t.Errorf("MaskExternalID is not deterministic: %q != %q", got, got2)
	}
	if got3 := MaskExternalID("cust-00124"); got3 == got {
		t.Errorf("MaskExternalID collided for different inputs: %q", got3)
	}
}

func TestMaskIP(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ipv4", "192.168.1.100", "192.168.1.***"},
		{"unparseable", "not-an-ip", "not-an-ip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskIP(tc.in); got != tc.want {
				t.Errorf("MaskIP(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
