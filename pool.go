package gpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMax     int64 = 1000000
	defaultMaxIdle int64 = 10000
)

// Pool implements a generic pool of objects. With generics this removes the ugly
// type checking required for xsync.Pool and also adds additional functionality like
// setting upper limits on active/idle objects and also allowing users to configure
// constructors, destructors and on get hooks to extra checking while accessing
// these objects
type Pool[T any] struct {
	newFn       func() (*T, error)
	onGetHookFn func(*T) error
	closeFn     func(*T) error

	idle             chan *T
	onGetHookMu      sync.Mutex
	onGetHookMap     map[*T]time.Time
	onGetHookTimeout time.Duration
	quit             chan struct{}

	active  int64
	maxIdle int64
	max     int64
}

type Option[T any] func(*Pool[T])

// WithNew allows user to configure a constructor for the object. If none is provided
// the default new constructor is used.
func WithNew[T any](newFn func() (*T, error)) Option[T] {
	return func(p *Pool[T]) {
		p.newFn = newFn
	}
}

// WithClose allows user to configure a custom closer for the objects.
func WithClose[T any](closeFn func(*T) error) Option[T] {
	return func(p *Pool[T]) {
		p.closeFn = closeFn
	}
}

// WithOnGetHook can be used to configure a custom hook to run on the object on every
// access from the pool.
func WithOnGetHook[T any](onGetHookFn func(*T) error) Option[T] {
	return func(p *Pool[T]) {
		p.onGetHookFn = onGetHookFn
	}
}

// WithOnGetHookTimeout provides same functionality as WithOnGetHook, but adds a timeout to
// the check. If the accesses are before the timeout they are not checked again. This can
// be used to avoid costly checks everytime for eg. network connections.
func WithOnGetHookTimeout[T any](onGetHookFn func(*T) error, timeout time.Duration) Option[T] {
	return func(p *Pool[T]) {
		p.onGetHookFn = onGetHookFn
		p.onGetHookTimeout = timeout
	}
}

// WithMax provides the option to set upper limit on the no. of objects managed by
// the pool.
func WithMax[T any](max int64) Option[T] {
	return func(p *Pool[T]) {
		if max < 0 {
			return
		}
		p.max = max
	}
}

// WithMaxIdle provides the option to set upper limit on the no. of idle objects
// maintained by the pool.
func WithMaxIdle[T any](maxIdle int64) Option[T] {
	return func(p *Pool[T]) {
		if maxIdle < 0 {
			return
		}
		p.maxIdle = maxIdle
	}
}

func New[T any](opts ...Option[T]) *Pool[T] {
	p := &Pool[T]{
		max:     defaultMax,
		maxIdle: defaultMaxIdle,
		newFn:   func() (*T, error) { return new(T), nil },
		closeFn: func(*T) error { return nil },
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.maxIdle > p.max {
		p.maxIdle = p.max
	}

	p.idle = make(chan *T, p.maxIdle)
	p.quit = make(chan struct{})
	p.onGetHookMap = make(map[*T]time.Time)

	return p
}

func (p *Pool[T]) Get(ctx context.Context) (*T, func(), error) {

	remove := func(o *T) {
		_ = p.closeFn(o)
		_ = atomic.AddInt64(&p.active, -1)

		p.onGetHookMu.Lock()
		delete(p.onGetHookMap, o)
		p.onGetHookMu.Unlock()
	}

	done := func(o *T) func() {
		return func() {
			// explicitly onGetHook for quit first
			select {
			case <-p.quit:
				remove(o)
				return
			default:
			}
			select {
			case p.idle <- o:
				if p.onGetHookTimeout != 0 {
					p.onGetHookMu.Lock()
					p.onGetHookMap[o] = time.Now().Add(p.onGetHookTimeout)
					p.onGetHookMu.Unlock()
				}
				return
			case <-p.quit:
			case <-time.After(300 * time.Millisecond):
				// if we are not able to enqueue it, close it
			}

			remove(o)
		}
	}

	for {
		select {
		case <-p.quit:
			return nil, nil, errors.New("pool stopped")
		default:
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case o, ok := <-p.idle:
			if !ok {
				continue
			}
			if p.onGetHookFn != nil {
				p.onGetHookMu.Lock()
				ts, ok := p.onGetHookMap[o]
				p.onGetHookMu.Unlock()

				// If there is no timeout for onGetHook, we need to onGetHook each entry
				// else only the entries that have not been onGetHooked for the timeout
				// duration
				if (ok && time.Now().After(ts)) || !ok {
					err := p.onGetHookFn(o)
					if err != nil {
						remove(o)
						continue
					}
				}
			}
			return o, done(o), nil
		default:
			// if there is no idle object, try to create a new one
		}

		if atomic.LoadInt64(&p.active) < p.max {
			o, err := p.newFn()
			if err != nil {
				return nil, nil, err
			}
			atomic.AddInt64(&p.active, 1)
			return o, done(o), nil
		}
	}
}

func (p *Pool[T]) Close() error {
	close(p.quit)
	close(p.idle)
	for o := range p.idle {
		_ = p.closeFn(o)
		_ = atomic.AddInt64(&p.active, -1)
	}

	start := time.Now()
	for {
		if atomic.LoadInt64(&p.active) == 0 {
			break
		}
		if time.Since(start) > 3*time.Second {
			return errors.New("waited 3 secs for all pool objects to be returned and cleaned up")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
