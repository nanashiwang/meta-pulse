package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miekg/dns"
	"github.com/nanashiwang/meta-pulse/services/dnsprobe/internal/probe"
)

func main() {
	service := probe.New()
	// Bind before readiness: HTTP health cannot pass if either DNS socket failed.
	udp, err := net.ListenPacket("udp", ":5353")
	if err != nil {
		log.Fatal(err)
	}
	defer udp.Close()
	tcp, err := net.Listen("tcp", ":5353")
	if err != nil {
		log.Fatal(err)
	}
	defer tcp.Close()
	servers := []*dns.Server{{PacketConn: udp, Handler: service, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second}, {Listener: tcp, Handler: service, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxTCPQueries: 5}}
	web := &http.Server{Addr: ":8091", Handler: service, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 4096}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 3)
	for _, server := range servers {
		go func(server *dns.Server) { errs <- server.ActivateAndServe() }(server)
	}
	go func() { errs <- web.ListenAndServe() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	log.Print("DNS observation started; no query or client address logging")
	for {
		select {
		case <-ticker.C:
			service.Sweep()
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = web.Shutdown(shutdown)
			for _, server := range servers {
				_ = server.ShutdownContext(shutdown)
			}
			return
		case err := <-errs:
			log.Print("listener stopped: ", err)
			os.Exit(1)
		}
	}
}
