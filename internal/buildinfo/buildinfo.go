// Package buildinfo exposes source identity without importing infrastructure into domain packages.
package buildinfo

// Info identifies a built artifact.
type Info struct {
	Revision string
}

// New returns immutable build identity.
func New(revision string) Info {
	return Info{Revision: revision}
}
