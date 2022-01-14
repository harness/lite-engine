package runtime

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

type StepOutput struct {
	mx          sync.Mutex
	fullOutput  *bytes.Buffer
	data        chan []byte
	done        <-chan struct{}
	subscribers map[chan []byte]struct{}
}

func NewStepOutput(ctx context.Context) *StepOutput {
	l := &StepOutput{
		mx:          sync.Mutex{},
		fullOutput:  &bytes.Buffer{},
		data:        make(chan []byte, 10), // nolint:gomnd
		done:        ctx.Done(),
		subscribers: make(map[chan []byte]struct{}),
	}

	go func() {
		for {
			select {
			case <-l.done:
				return
			case data := <-l.data:
				func() {
					l.mx.Lock()
					defer l.mx.Unlock()

					l.fullOutput.Write(data)

					for ch := range l.subscribers {
						select {
						case <-l.done:
							return
						case ch <- data:
						}
					}
				}()
			}
		}
	}()

	return l
}

func (l *StepOutput) Done() <-chan struct{} {
	return l.done
}

func (l *StepOutput) Write(data []byte) (int, error) {
	select {
	case l.data <- data:
		return len(data), nil
	case <-l.done:
		return 0, nil
	}
}

// Subscribe returns the output log that has been created so far (from the offset position) and
// it registers the ch channel to receive further data output.
func (l *StepOutput) Subscribe(ch chan []byte, offset int) (data []byte, err error) {
	l.mx.Lock()
	data = l.fullOutput.Bytes()
	l.subscribers[ch] = struct{}{}
	l.mx.Unlock()

	if offset > len(data) {
		data = nil
		err = fmt.Errorf("error: index 'offset' is out of bounds Offset=%d Total=%d", offset, len(data))
	} else {
		data = data[offset:]
	}

	return
}

func (l *StepOutput) Unsubscribe(ch chan []byte) {
	l.mx.Lock()
	delete(l.subscribers, ch)
	l.mx.Unlock()
}
