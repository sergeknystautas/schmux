package workspace

import "testing"

// ---------------------------------------------------------------------------
// LinearSyncFromDefault happy-path tests (1-6)
// ---------------------------------------------------------------------------

// Test 1: Feature branch has local commits, main hasn't moved.
// Expect success with count=0 (nothing to rebase).
func TestLinearSyncFromDefault_AlreadyUpToDate(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:hello")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 0)
	fix.AssertInvariants()
}

// Test 2: Stay on main, no divergence at all.
// Expect success with count=0.
func TestLinearSyncFromDefault_SameCommit(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("main").
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 0)
	fix.AssertInvariants()
}

// Test 3: No local commits on feature, 1 remote commit on main.
// Expect success with count=1 and origin/main is ancestor of HEAD.
func TestLinearSyncFromDefault_StrictlyBehind_SingleCommit(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote update", "remote.txt:data")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertAncestorOf("origin/main", "HEAD")
	fix.AssertInvariants()
}

// Test 4: No local commits, 5 remote commits on separate files.
// Expect success with count=5.
func TestLinearSyncFromDefault_StrictlyBehind_ManyCommits(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(
			tc("remote 1", "r1.txt:content1"),
			tc("remote 2", "r2.txt:content2"),
			tc("remote 3", "r3.txt:content3"),
			tc("remote 4", "r4.txt:content4"),
			tc("remote 5", "r5.txt:content5"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 5)
	fix.AssertInvariants()
}

// Test 5: 1 local commit on local.txt, 1 remote commit on remote.txt (no conflict).
// Expect success with count=1 and origin/main is ancestor of HEAD.
func TestLinearSyncFromDefault_Diverged_NoConflicts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local change", "local.txt:local content")).
		WithRemoteCommits(tc("remote change", "remote.txt:remote content")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertAncestorOf("origin/main", "HEAD")
	fix.AssertInvariants()
}

// Test 6: 3 local commits (l1-l3.txt), 5 remote commits (r1-r5.txt), all different files.
// Expect success with count=5 and origin/main is ancestor of HEAD.
func TestLinearSyncFromDefault_Diverged_ManyCommitsBothSides(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(
			tc("local 1", "l1.txt:local1"),
			tc("local 2", "l2.txt:local2"),
			tc("local 3", "l3.txt:local3"),
		).
		WithRemoteCommits(
			tc("remote 1", "r1.txt:remote1"),
			tc("remote 2", "r2.txt:remote2"),
			tc("remote 3", "r3.txt:remote3"),
			tc("remote 4", "r4.txt:remote4"),
			tc("remote 5", "r5.txt:remote5"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 5)
	fix.AssertAncestorOf("origin/main", "HEAD")
	fix.AssertInvariants()
}
