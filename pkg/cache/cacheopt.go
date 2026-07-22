package cache

import "time"

const (
	defaultExpiry         = time.Hour * 24 * 7
	defaultNotFoundExpiry = time.Minute
)

type (
	// Options is used to store the cache options.
	Options struct {
		Expiry         time.Duration
		NotFoundExpiry time.Duration
		Invalidations  *InvalidationQueue
	}

	// Option defines the method to customize an Options.
	Option func(o *Options)
)

func newOptions(opts ...Option) Options {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}

	if o.Expiry <= 0 {
		o.Expiry = defaultExpiry
	}
	if o.NotFoundExpiry <= 0 {
		o.NotFoundExpiry = defaultNotFoundExpiry
	}

	return o
}

// WithExpiry returns a func to customize an Options with given expiry.
func WithExpiry(expiry time.Duration) Option {
	return func(o *Options) {
		o.Expiry = expiry
	}
}

// WithNotFoundExpiry returns a func to customize an Options with given not found expiry.
func WithNotFoundExpiry(expiry time.Duration) Option {
	return func(o *Options) {
		o.NotFoundExpiry = expiry
	}
}

// WithInvalidationQueue defers cache-key invalidation until the owner flushes
// the queue. It is used by database transactions so cache entries cannot be
// repopulated from data that has not committed yet.
func WithInvalidationQueue(queue *InvalidationQueue) Option {
	return func(o *Options) {
		o.Invalidations = queue
	}
}
