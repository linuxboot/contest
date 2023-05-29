package signaling

import (
	"context"
	"fmt"
	"sync"
)

type ctxKey[T Signal] struct {
	Signal T
}

// Signal is an interface defining constraints of a type which could be used
// as a signal (in functions WithSignal, Until and IsSignaledWith).
type Signal interface {
	error
	comparable
}

type signaler chan struct{}

func getSignalChan[T Signal](ctx context.Context, signal T) signaler {
	signaler, _ := ctx.Value(ctxKey[T]{Signal: signal}).(signaler)
	// If a signaller is not defined then a nil is returned,
	// and a nil channel is an infinitely open channel without events,
	// so it blocks reading forever.
	return signaler
}

func withSignalChan[T Signal](ctx context.Context, signal T) (context.Context, signaler) {
	s := make(signaler)
	return context.WithValue(ctx, ctxKey[T]{Signal: signal}, s), s
}

// IsSignaledWith returns true if the context received the signal.
func IsSignaledWith[T Signal](ctx context.Context, signal T) (bool, error) {
	return chanIsClosed(getSignalChan(ctx, signal))
}

func chanIsClosed(c <-chan struct{}) (bool, error) {
	select {
	case _, isOpen := <-c:
		if isOpen {
			// Any signaling here supposed to be broadcast, and the only broadcast
			// kind of event on a channel is a closure. So no events should be sent
			// through this channel, but it could be closed.
			return false, fmt.Errorf("the channel has an event, but is not closed; this was supposed to be impossible")
		}
		return true, nil
	default:
		return false, nil
	}
}

// Until works similar to Done(), but waits for a specific signal
// defined via WithSignal function.
func Until[T Signal](ctx context.Context, signal T) <-chan struct{} {
	return getSignalChan(ctx, signal)
}

// SignalFunc is similar to context.CancelFunc, but instead of closing
// the context it sends a signal through it (which could be observed with Until
// and IsSignaledWith).
type SignalFunc func()

var _ = context.CancelFunc((SignalFunc)(nil)) // check if CancelFunc and SignalFunc has the same signature

// WithSignal is similar to context.WithCancel, but the returned function
// sends signal through the context (which could be observed with Until
// and IsSignaledWith).
func WithSignal[T Signal](ctx context.Context, signal T) (context.Context, SignalFunc) {
	ctx, s := withSignalChan(ctx, signal)
	var closeOnce sync.Once
	return ctx, func() {
		closeOnce.Do(func() {
			close(s)
		})
	}
}
