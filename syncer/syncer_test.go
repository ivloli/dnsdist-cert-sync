package syncer

import "testing"

func TestParsePayloadNewSchema(t *testing.T) {
	content := `{
  "certificate_pem": "CERT",
  "private_key_pem": "KEY",
  "certificate_fullchain_pem": "FULL"
}`
	p, err := parsePayload(content)
	if err != nil {
		t.Fatalf("parsePayload error: %v", err)
	}
	if p.Cert != "CERT" {
		t.Fatalf("cert = %q", p.Cert)
	}
	if p.Key != "KEY" {
		t.Fatalf("key = %q", p.Key)
	}
	if p.FullChain != "FULL" {
		t.Fatalf("fullchain = %q", p.FullChain)
	}
}
