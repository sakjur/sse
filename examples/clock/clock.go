package main

import (
	"github.com/sakjur/sse"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	stream := &sse.Stream{
		Handler: func(w *sse.Worker, r *http.Request) {
			ticker := time.NewTicker(1 * time.Second)
			for range ticker.C {
				log.Println("tick")
				select {
				case <-r.Context().Done():
					return
				default:
					err := w.Event("time", strings.NewReader(time.Now().UTC().Format(time.RFC3339)))
					if err != nil {
						log.Println("got error: ", err)
						panic(err)
					}
				}
			}
		},
	}

	s := &http.Server{
		Addr:              ":8080",
		ReadHeaderTimeout: 5 * time.Second,
	}
	http.Handle("/time", stream)
	s.ListenAndServe()
}
