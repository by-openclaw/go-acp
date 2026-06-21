package ws

// Test-only helpers to install the seam hooks (see seam.go). Each returns a
// restore func that clears the hook; install with `defer setX(f)()`. Atomic-
// backed so installing/clearing a hook never data-races a live per-connection
// decode goroutine (the reason the seams moved off plain package vars).

func setRandRead(f func(b []byte) (int, error)) func() {
	randReadHook.Store(&f)
	return func() { randReadHook.Store(nil) }
}

func setPlenSeam(f func(plen int64) int64) func() {
	plenSeamHook.Store(&f)
	return func() { plenSeamHook.Store(nil) }
}
