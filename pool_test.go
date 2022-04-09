package gpool_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aloknerurkar/gpool"
)

type obj1 struct {
	val int
}

func TestNew(t *testing.T) {
	p := gpool.New[obj1]()

	err := p.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBasicPool(t *testing.T) {
	p := gpool.New[obj1]()

	t.Cleanup(func() {
		err := p.Close()
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("get same object", func(t *testing.T) {
		o, done, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if o == nil {
			t.Fatal("pool returned nil obj")
		}
		o.val = 100
		done()

		if p.Active() != 1 {
			t.Fatal("invalid active count")
		}

		o2, done2, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if o2.val != 100 {
			t.Fatal("pool returned different item")
		}
		if p.Active() != 1 {
			t.Fatal("incorrect active count")
		}
		o2.val = 0
		done2()
	})

	t.Run("get new objects", func(t *testing.T) {
		dones := []func(){}
		for i := 0; i < 10; i++ {
			o, done, err := p.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// all values will be initialized to 0
			if o.val != 0 {
				t.Fatal("got unknown value")
			}
			dones = append(dones, done)
		}

		if p.Active() != 10 {
			t.Fatal("incorrect active count")
		}

		for _, d := range dones {
			d()
		}

		// ensure once all objects are returned, no new objects are created
		// this time keep returning objects to idle pool
		for i := 0; i < 10; i++ {
			o, done, err := p.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// all values will be initialized to 0
			if o.val != 0 {
				t.Fatal("got unknown value")
			}
			done()
		}

		if p.Active() != 10 {
			t.Fatal("incorrect active count")
		}
	})
}

func TestPoolMaxAndIdle(t *testing.T) {
	newCalls := 0
	closeCalls := 0

	p := gpool.New[obj1](
		gpool.WithNew[obj1](func() (*obj1, error) {
			newCalls++
			return new(obj1), nil
		}),
		gpool.WithClose[obj1](func(*obj1) error {
			closeCalls++
			return nil
		}),
		gpool.WithMax[obj1](10),
		gpool.WithMaxIdle[obj1](5),
	)

	t.Cleanup(func() {
		err := p.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeCalls != 15 {
			t.Fatal("incorrect no of close calls")
		}
	})

	// Create max no of objects
	dones := []func(){}
	for i := 0; i < 10; i++ {
		o, done, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		// all values will be initialized to 0
		if o.val != 0 {
			t.Fatal("got unknown value")
		}
		dones = append(dones, done)
	}

	if p.Active() != 10 {
		t.Fatal("incorrect active count")
	}

	if newCalls != 10 {
		t.Fatal("new function not called")
	}

	defer func() {
		for _, d := range dones {
			d()
		}
	}()

	t.Run("get failure with context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(time.Second)
			cancel()
		}()

		_, _, err := p.Get(ctx)
		if err != ctx.Err() {
			t.Fatal(err)
		}
	})

	t.Run("get waits for next available object", func(t *testing.T) {
		d := dones[0]
		dones = dones[1:]

		for i := 0; i < 3; i++ {
			go func() {
				time.Sleep(time.Second)
				d()
			}()

			_, done, err := p.Get(context.Background())
			if err != nil {
				t.Fatal("expected successful get", err)
			}

			d = done
		}

		d()
	})

	t.Run("only idle count objects are maintained", func(t *testing.T) {
		for _, d := range dones {
			d()
		}
		dones = nil
		if closeCalls != 5 {
			t.Fatal("expected 5 close calls, found", closeCalls)
		}
		if p.Active() != 5 {
			t.Fatal("expected active count 5, found", p.Active())
		}
		for i := 0; i < 5; i++ {
			_, d, err := p.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dones = append(dones, d)
		}
		if p.Active() != 5 {
			t.Fatal("expected active count 5, found", p.Active())
		}
		if newCalls != 10 {
			t.Fatal("expected no new calls")
		}
	})

	t.Run("new object when no idle left", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			_, d, err := p.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dones = append(dones, d)
		}
		if p.Active() != 10 {
			t.Fatal("expected active count 10, found", p.Active())
		}
		if newCalls != 15 {
			t.Fatal("expected new calls 15, found", newCalls)
		}
	})
}

func TestPoolOnGetHook(t *testing.T) {
	onGetHookCalls := 0

	p := gpool.New[obj1](
		gpool.WithMax[obj1](10),
		gpool.WithOnGetHookTimeout[obj1](func(*obj1) error {
			onGetHookCalls++
			return nil
		}, time.Second),
	)

	// Create max no of objects
	dones := []func(){}
	for i := 0; i < 10; i++ {
		_, done, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		dones = append(dones, done)
	}

	if p.Active() != 10 {
		t.Fatal("incorrect active count")
	}

	t.Run("no onGetHook call within timeout", func(t *testing.T) {
		for _, d := range dones {
			d()
		}

		dones = nil

		for i := 0; i < 10; i++ {
			_, done, err := p.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dones = append(dones, done)
		}

		if onGetHookCalls != 0 {
			t.Fatal("expected no onGetHookCalls, found", onGetHookCalls)
		}
	})

	t.Run("onGetHook calls after timeout", func(t *testing.T) {
		for _, d := range dones {
			d()
		}

		time.Sleep(2 * time.Second)

		dones = nil

		for i := 0; i < 10; i++ {
			_, done, err := p.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dones = append(dones, done)
		}

		if onGetHookCalls != 10 {
			t.Fatal("expected 10 onGetHookCalls, found", onGetHookCalls)
		}
	})

	t.Run("no onGetHook calls after second done", func(t *testing.T) {
		for _, d := range dones {
			d()
		}

		dones = nil

		for i := 0; i < 10; i++ {
			_, done, err := p.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dones = append(dones, done)
		}

		if onGetHookCalls != 10 {
			t.Fatal("expected 10 onGetHookCalls, found", onGetHookCalls)
		}
	})

	t.Run("return objects after Close", func(t *testing.T) {
		startedClose := make(chan struct{})
		wg := sync.WaitGroup{}
		errc := make(chan error, 1)

		wg.Add(1)
		go func() {
			defer wg.Done()
			close(startedClose)

			err := p.Close()
			if err != nil {
				errc <- err
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startedClose
			for _, d := range dones {
				d()
			}
		}()

		wg.Wait()
		select {
		case err := <-errc:
			t.Fatal(err)
		default:
		}
	})
}

func TestPoolErrors(t *testing.T) {

	failNew := true
	onGetHookFailed := false

	p := gpool.New[obj1](
		gpool.WithNew[obj1](func() (*obj1, error) {
			if failNew {
				failNew = false
				return nil, errors.New("dummy new error")
			}
			return new(obj1), nil
		}),
		gpool.WithMax[obj1](10),
		gpool.WithOnGetHook[obj1](func(*obj1) error {
			if !onGetHookFailed {
				onGetHookFailed = true
				return errors.New("dummy get error")
			}
			return nil
		}),
	)

	t.Run("new fails then succeeds", func(t *testing.T) {
		_, _, err := p.Get(context.Background())
		if err == nil || err.Error() != "dummy new error" {
			t.Fatalf("unexpected error %v", err)
		}

		_, done, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		done()
	})

	t.Run("onGetHook fails then returns new", func(t *testing.T) {
		_, done, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if !onGetHookFailed {
			t.Fatal("expected ongethook failure")
		}

		done()
	})

	t.Run("get fails after close and close returns error on active objects", func(t *testing.T) {
		// dont return this object to fail close
		_, _, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		wg := sync.WaitGroup{}
		startedClose := make(chan struct{})
		errc := make(chan error, 2)

		wg.Add(1)
		go func() {
			defer wg.Done()
			close(startedClose)
			err := p.Close()
			if err == nil {
				errc <- errors.New("expected error on close")
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startedClose
			_, _, err := p.Get(context.Background())
			if err == nil {
				errc <- errors.New("expected error on get after close")
			}
		}()

		wg.Wait()
		select {
		case err := <-errc:
			t.Fatal(err)
		default:
		}
	})
}
