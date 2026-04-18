package dashboard

import "testing"

// Today's middleware (validateAutolearnRepo) rejects "owner.repo" because of
// strings.ContainsAny(repo, "/\\.\x00"). After Step 6 switches to isValidRepoName,
// "owner.repo" should be accepted while traversal patterns stay rejected.
//
// This is a pure validator test — it doesn't exercise the middleware itself.
// The middleware change (Step 6) is verified by manual reasoning + the existing
// autolearn handler tests not breaking.
func TestAutolearnRepoValidationAcceptsDottedRepoNames(t *testing.T) {
	if !isValidRepoName("owner.repo") {
		t.Errorf("owner.repo rejected by isValidRepoName, want accepted")
	}
	if isValidRepoName("../etc") {
		t.Errorf("../etc accepted by isValidRepoName, want rejected")
	}
	if isValidRepoName(".foo") {
		t.Errorf(".foo accepted by isValidRepoName, want rejected (leading dot)")
	}
}
