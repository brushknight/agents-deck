package server

// restrictSocket: on Windows file modes don't limit who connects. The socket
// sits in the data folder under the user's profile (or %TEMP%, also under
// it), which Windows keeps private to that user, so nothing more is done.
func restrictSocket(path string) error { return nil }
