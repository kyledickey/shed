package logtail

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestFollow(t *testing.T) {
	tail := New(3)
	fmt.Fprint(tail, "one\ntwo\nthr")
	fmt.Fprint(tail, "ee\nfour\nfi")

	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan string, 10)
	done := make(chan struct{})
	go func() {
		tail.Follow(ctx, func(line string) { got <- line })
		close(done)
	}()

	var lines []string
	for range 3 {
		lines = append(lines, <-got)
	}
	if want := []string{"two", "three", "four"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("history = %q, want %q", lines, want)
	}

	fmt.Fprint(tail, "ve\n")
	select {
	case line := <-got:
		if line != "five" {
			t.Errorf("followed line = %q, want five", line)
		}
	case <-time.After(time.Second):
		t.Fatal("no followed line")
	}

	cancel()
	<-done
	if n := len(tail.subs); n != 0 {
		t.Errorf("%d followers left after Follow returned", n)
	}
}

func TestLines(t *testing.T) {
	tail := New(3)
	fmt.Fprint(tail, "one\ntwo\nthree\nfour\nfi")
	tests := []struct {
		n    int
		want []string
	}{
		{0, []string{}},
		{-1, []string{}},
		{2, []string{"three", "four"}},
		{3, []string{"two", "three", "four"}},
		{10, []string{"two", "three", "four"}},
	}
	for _, tt := range tests {
		if got := tail.Lines(tt.n); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Lines(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
	if got := New(3).Lines(5); len(got) != 0 {
		t.Errorf("Lines of an empty tail = %q", got)
	}
}

func TestSlowFollowerDoesNotBlock(t *testing.T) {
	tail := New(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var once sync.Once
	go tail.Follow(ctx, func(string) {
		once.Do(func() { close(started) })
		<-ctx.Done()
	})
	fmt.Fprint(tail, "first\n")
	<-started

	written := make(chan struct{})
	go func() {
		for i := range followBuffer * 2 {
			fmt.Fprintf(tail, "%d\n", i)
		}
		close(written)
	}()
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("writes blocked on a slow follower")
	}
}
