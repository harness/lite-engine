package safego

import (
	"context"
	"runtime/debug"
	"sync"

	"github.com/sirupsen/logrus"
)

func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithField("goroutine", name).WithField("panic", r).
					WithField("stack", string(debug.Stack())).
					Errorln("Goroutine panic recovered")
			}
		}()
		fn()
	}()
}

func SafeGoWithContext(name string, ctx context.Context, fn func(context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithField("goroutine", name).WithField("panic", r).
					WithField("stack", string(debug.Stack())).
					Errorln("Goroutine panic recovered")
			}
		}()
		fn(ctx)
	}()
}

func SafeGoWithWaitGroup(name string, wg *sync.WaitGroup, fn func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				logrus.WithField("goroutine", name).WithField("panic", r).
					WithField("stack", string(debug.Stack())).
					Errorln("Goroutine panic recovered")
			}
		}()
		fn()
	}()
}
