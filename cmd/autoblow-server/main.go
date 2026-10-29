package main

import (
	"autoblow.local/emulator/core"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	config := flag.String("config", "", "path to JSON configuration")
	version := flag.Bool("version", false, "print version and exit")
	health := flag.Bool("healthcheck", false, "check the local API and exit")
	flag.Parse()
	if *version {
		fmt.Println(core.Version)
		return
	}
	cfg := core.DefaultConfig()
	if *config != "" {
		f, err := os.Open(*config)
		if err != nil {
			fatal(err)
		}
		dec := json.NewDecoder(f)
		dec.DisallowUnknownFields()
		err = dec.Decode(&cfg)
		if err == nil && dec.Decode(new(any)) != io.EOF {
			err = fmt.Errorf("trailing configuration data")
		}
		f.Close()
		if err != nil {
			fatal(err)
		}
	}
	if *health {
		host, port, err := net.SplitHostPort(cfg.APIListen)
		if err != nil {
			fatal(err)
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		client := http.Client{Timeout: 2 * time.Second}
		r, err := client.Get("http://" + net.JoinHostPort(host, port) + "/health")
		if err != nil {
			fatal(err)
		}
		defer r.Body.Close()
		var status struct {
			Version string `json:"version"`
		}
		if r.StatusCode != 200 {
			fatal(fmt.Errorf("health HTTP %d", r.StatusCode))
		}
		if err = json.NewDecoder(r.Body).Decode(&status); err != nil {
			fatal(err)
		}
		if status.Version != core.Version {
			fatal(fmt.Errorf("unexpected server version"))
		}
		return
	}
	s, err := core.New(cfg)
	if err != nil {
		fatal(err)
	}
	if err = s.Start(); err != nil {
		fatal(err)
	}
	fmt.Printf("Autoblow Local %s ready; UI http://%s; local authorization file: %s/control-token\n", core.Version, s.Address(), cfg.DataDir)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case <-s.ShutdownRequested():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	if err = s.Close(shutdown); err != nil {
		fatal(fmt.Errorf("shutdown STOP unconfirmed: %w", err))
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
