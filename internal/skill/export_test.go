package skill

// SetRename replaces os.Rename inside Install for a test.
func SetRename(f func(oldpath, newpath string) error) (restore func()) {
	old := rename
	rename = f
	return func() { rename = old }
}
