package ui

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner shows a terminal spinner with a message while work is being done.
type Spinner struct {
	msg    string
	done   chan struct{}
	wg     sync.WaitGroup
}

// NewSpinner creates and starts a spinner with the given message.
func NewSpinner(msg string) *Spinner {
	s := &Spinner{
		msg:  msg,
		done: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer s.wg.Done()
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			// Clear the spinner line
			fmt.Printf("\r\033[K")
			return
		case <-ticker.C:
			fmt.Printf("\r%s %s", spinnerFrames[i%len(spinnerFrames)], s.msg)
			i++
		}
	}
}

// Stop stops the spinner and clears the line.
func (s *Spinner) Stop() {
	close(s.done)
	s.wg.Wait()
}

// StopWithMessage stops the spinner and prints a final message.
func (s *Spinner) StopWithMessage(msg string) {
	close(s.done)
	s.wg.Wait()
	fmt.Println(msg)
}
