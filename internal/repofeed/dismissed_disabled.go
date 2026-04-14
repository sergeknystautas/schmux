//go:build norepofeed

package repofeed

type DismissedStore struct{}

func NewDismissedStore() *DismissedStore                       { return &DismissedStore{} }
func (ds *DismissedStore) Dismiss(_ string, _ string)          {}
func (ds *DismissedStore) IsDismissed(_ string, _ string) bool { return false }
