package backoff

import "time"

type Backoff struct {
	op       func() error
	max      int
	interval int
}

func NewBackoff(op func() error, max int, interval int) *Backoff {
	return &Backoff{
		op:       op,
		max:      max,
		interval: interval,
	}
}

func (b *Backoff) Do() error {
	for i := 0; i < b.max; i++ {
		if err := b.op(); err == nil {
			return nil
		}
		time.Sleep(time.Duration(b.interval) * time.Millisecond)
	}
	return nil
}
