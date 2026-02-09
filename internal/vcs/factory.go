package vcs

// NewCommandBuilder returns a CommandBuilder for the given VCS type.
// Defaults to git if vcsType is empty or unrecognized.
func NewCommandBuilder(vcsType string) CommandBuilder {
	switch vcsType {
	case "sapling":
		return &SaplingCommandBuilder{}
	default:
		return &GitCommandBuilder{}
	}
}
