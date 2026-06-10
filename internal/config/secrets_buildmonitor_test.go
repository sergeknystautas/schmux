package config

import "testing"

func TestGitHubIdentityRoundTrip(t *testing.T) {
	setupSecretsHome(t)
	if err := SaveGitHubIdentity("Octocat", "tok_abc", "repo"); err != nil {
		t.Fatal(err)
	}
	logins, err := GetGitHubIdentityLogins()
	if err != nil {
		t.Fatal(err)
	}
	if len(logins) != 1 || logins[0] != "octocat" {
		t.Fatalf("logins=%v", logins)
	}
	tok, err := GetGitHubToken("octocat")
	if err != nil || tok != "tok_abc" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
}
