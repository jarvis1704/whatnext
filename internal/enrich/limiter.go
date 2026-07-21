package enrich

import "time"

// newLimiter returns a channel that yields a token every interval, capping
// the combined rate of every concurrent worker to one TMDb request sequence
// per interval — the same politeness budget a plain time.Sleep enforced in
// the old sequential loop, just shared across goroutines instead of paced
// by one. Call stop when done to release the underlying ticker goroutine.
func newLimiter(interval time.Duration) (tokens <-chan struct{}, stop func()) {
	ch := make(chan struct{})
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				select {
				case ch <- struct{}{}:
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()
	return ch, func() { close(done) }
}
