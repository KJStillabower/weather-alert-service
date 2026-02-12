package lifecycle

import "sync/atomic"

var shuttingDown atomic.Bool

// SetShuttingDown sets the shutdown flag. Call when SIGTERM/SIGINT received.
// Health handler returns 503 with status shutting-down while true.
func SetShuttingDown(v bool) {
	shuttingDown.Store(v)
}

// IsShuttingDown returns true if the process is draining and should not receive new traffic.
func IsShuttingDown() bool {
	return shuttingDown.Load()
}
