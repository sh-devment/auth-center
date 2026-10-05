package main

import "testing"

// Twin of TestDerivedSecretMatchesAuthCenter in auth-miniapp. The two services
// derive this value separately and never exchange the token it comes from, so a
// drift on either side shows up at runtime only as an unexplained 403.
func TestMiniappSecretMatchesMiniapp(t *testing.T) {
	const (
		token = "123456:test-bot-token"
		want  = "d1200c0ddc8b9375df415693b849be3d6a4c7079cc50f73e40ca1bdb0b53fe89"
	)
	if got := miniappSecretFrom(token); got != want {
		t.Fatalf("miniapp secret drifted:\n got  %s\n want %s", got, want)
	}
}
