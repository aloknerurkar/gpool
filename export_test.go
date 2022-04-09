package gpool

import "sync/atomic"

func (p *Pool[T]) Active() int64 {
	return atomic.LoadInt64(&p.active)
}
