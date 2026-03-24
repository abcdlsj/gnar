package norm

import "testing"

func TestNameAndTenant(t *testing.T) {
	if got := Name(" Demo App "); got != "demo-app" {
		t.Fatalf("name = %q", got)
	}
	if got := Tenant(""); got != "default" {
		t.Fatalf("tenant = %q", got)
	}
}

func TestHostAndDomains(t *testing.T) {
	if got := Host("https://API.Example.com:443/path"); got != "api.example.com" {
		t.Fatalf("host = %q", got)
	}

	values := Domains([]string{"api.example.com", "https://api.example.com", "web.example.com:443"})
	if len(values) != 2 || values[0] != "api.example.com" || values[1] != "web.example.com" {
		t.Fatalf("domains = %#v", values)
	}
}

func TestSuffixesAndKey(t *testing.T) {
	values := Suffixes([]string{"example.com", "*.example.com", ".internal.example.com"})
	if len(values) != 2 || values[0] != ".example.com" || values[1] != ".internal.example.com" {
		t.Fatalf("suffixes = %#v", values)
	}

	if got := Key(" Team A ", " My API "); got != "team-a/my-api" {
		t.Fatalf("key = %q", got)
	}
}
