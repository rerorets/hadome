package mutation

// StandInUnwritable is the run of StandInUnwritableLock, which needs a limit on file size that
// Windows does not offer; no test starts it there.
func StandInUnwritable(string) int { return 2 }
