package sse

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ContentTypeEventStream = "text/event-worker"

type Stream struct {
	Handler Handler
}

type Worker struct {
	lock  sync.Mutex
	in    io.Writer
	out   io.Reader
	flush chan struct{}
}

type Handler func(w *Worker, r *http.Request)

func (s *Stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var flusher http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flusher = f
	} else {
		log.Println("[error] http.ResponseWriter does not implement flusher")
		w.WriteHeader(http.StatusInternalServerError)
	}
	w.Header().Add("Content-Type", ContentTypeEventStream)

	channel := newWriter()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-channel.flush:
				flusher.Flush()
			case <-ticker.C:
				channel.ping()
			}
		}
	}()

	ctx, cancel := context.WithCancel(r.Context())
	go s.Handler(channel, r.WithContext(ctx))

	_, err := io.Copy(w, channel.out)
	if err != nil {
		log.Println("copy error: ", err)
		// TODO Handle error
	}
	cancel()
}

func (s *Worker) Event(name string, r io.Reader) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if name != "" {
		name = strings.Replace(name, "\n", "\\n", -1)

		_, err := copyAndReplace(s.in, strings.NewReader(fmt.Sprintf("event: %s\n", name)), nil, nil)
		if err != nil {
			return fmt.Errorf("error writing event name: %w", err)
		}
	}

	_, err := copyAndReplace(s.in, strings.NewReader("data: "), nil, nil)
	if err != nil {
		return err
	}

	_, err = copyAndReplace(s.in, r, []byte{'\n'}, []byte("\ndata: "))
	if err != nil {
		return err
	}

	_, err = copyAndReplace(s.in, strings.NewReader("\n\n"), nil, nil)
	s.flush <- struct{}{}
	return err
}

func (s *Worker) ping() {
	s.lock.Lock()
	_, err := s.in.Write([]byte(": ping\n\n"))
	if err != nil {
		log.Println("got error while pinging: ", err)
	}
	s.lock.Unlock()
}

func copyAndReplace(dst io.Writer, src io.Reader, replace []byte, new []byte) (total int, err error) {
	buf := make([]byte, 2*1024)

	for {
		var n int
		n, err = src.Read(buf)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
		if n == 0 {
			continue
		}

		sub := buf[:n]
		var out []byte
		if replace != nil {
			splits := bytes.Split(sub, replace)
			out = bytes.Join(splits, new)
		} else {
			out = sub
		}

		written := 0

		for written < len(out) {
			n, err = dst.Write(out)
			if err != nil {
				break
			}
			total += n
			out = out[n:]
		}
	}

	return
}

func newWriter() *Worker {
	r, w := io.Pipe()
	return &Worker{
		in:    w,
		out:   r,
		flush: make(chan struct{}),
	}
}
