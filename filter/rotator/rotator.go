// Package rotator is used to rotate filter by period.
package rotator

import (
	"context"
	"github.com/x0rworld/go-bloomfilter/config"
	"github.com/x0rworld/go-bloomfilter/core"
	"github.com/x0rworld/go-bloomfilter/filter"
	"sync/atomic"
	"time"
)

// NewFilterFunc returns filter that will be performed by Rotator in handleRotating.
// It's the same signature with factory.FilterFactory.NewFilter.
type NewFilterFunc func(ctx context.Context) (filter.Filter, error)

type filterPair struct {
	current filter.Filter
	next    filter.Filter
}

type Rotator struct {
	ctx       context.Context
	cfg       config.RotatorConfig
	newFilter NewFilterFunc
	// type: *filterPair
	pair atomic.Value
}

func (r *Rotator) handleRotating(freq time.Duration) {
	for {
		current := time.Now()
		next := current.Add(freq).Truncate(freq)
		timer := time.NewTimer(next.Sub(current))
		select {
		case <-timer.C:
			r.rotate()
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Rotator) rotate() error {
	newFilter, err := r.genFilter(true)
	if err != nil {
		return err
	}

	oldPair := r.pair.Load().(*filterPair)
	newPair := &filterPair{
		current: oldPair.next,
		next:    newFilter,
	}
	r.pair.Store(newPair)
	return err
}

func (r *Rotator) Exist(data string) (bool, error) {
	return r.pair.Load().(*filterPair).current.Exist(data)
}

func (r *Rotator) Add(data string) error {
	p := r.pair.Load().(*filterPair)
	err := p.current.Add(data)
	if err != nil {
		return err
	}
	return p.next.Add(data)
}

func (r *Rotator) genFilter(isNext bool) (filter.Filter, error) {
	// currently, only RedisBitmapFactory.NewBitmap() refers the value.
	val := core.BitmapFactoryCtxValue{
		IsRotatorEnabled: r.cfg.Enable,
		IsNextFilter:     isNext,
		RotatorMode:      r.cfg.Mode,
		Now:              time.Now(),
	}
	vCtx := context.WithValue(r.ctx, core.BitmapFactoryCtxKey, val)
	f, err := r.newFilter(vCtx)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (r *Rotator) genFilterPair() (*filterPair, error) {
	current, err := r.genFilter(false)
	if err != nil {
		return nil, err
	}
	next, err := r.genFilter(true)
	if err != nil {
		return nil, err
	}
	return &filterPair{
		current: current,
		next:    next,
	}, nil
}

// NewRotator returns *Rotator that rotates filter by period, all rotating filters will be generated by newFilter.
func NewRotator(ctx context.Context, cfg config.RotatorConfig, newFilter NewFilterFunc) (*Rotator, error) {
	r := &Rotator{
		ctx:       ctx,
		cfg:       cfg,
		newFilter: newFilter,
	}

	p, err := r.genFilterPair()
	if err != nil {
		return nil, err
	}
	r.pair.Store(p)

	go r.handleRotating(cfg.Freq)

	return r, nil
}
