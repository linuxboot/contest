package signals

type pausedType struct{}

func (pausedType) Error() string { return "paused" }

// Paused is a signal (signaling.Signal) to notify goroutines
// that the process is trying to pause and all long-running
// routines should save their state (if required) and return.
var Paused = pausedType{}
