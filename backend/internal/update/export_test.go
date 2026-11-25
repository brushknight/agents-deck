package update

import "os"

// FailRename makes renames of paths that match fail with err until the
// returned function is called (tests only).
func FailRename(match func(from, to string) bool, err error) func() {
	rename = func(from, to string) error {
		if match(from, to) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
		}
		return os.Rename(from, to)
	}
	return func() { rename = os.Rename }
}
