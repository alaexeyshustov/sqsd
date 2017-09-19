package sqsd

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"sync"
)

type SQSStatServer struct {
	Srv *http.Server
}

func NewStatServer(m *http.ServeMux, p int) *SQSStatServer {
	return &SQSStatServer{
		Srv: &http.Server{
			Addr:    ":" + strconv.Itoa(p),
			Handler: m,
		},
	}
}

func (s *SQSStatServer) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println("stat server start.")

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.Srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	if err := s.Srv.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}

	log.Println("stat server closed.")
}
