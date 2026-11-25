package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kejith/wow-tools-forever/syncserver/internal/api"
	"github.com/kejith/wow-tools-forever/syncserver/internal/db"
	"github.com/kejith/wow-tools-forever/syncserver/internal/publish"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	addr := envOr("LISTEN_ADDR", "127.0.0.1:8090")
	blobDir := envOr("BLOB_DIR", "/var/lib/wow-ah-sync/blobs")
	cooldownSec := envInt("UPLOAD_COOLDOWN_SEC", 600)
	ipHourlyLimit := envInt("IP_HOURLY_LIMIT", 20)
	publishSec := envInt("PUBLISH_INTERVAL_SEC", 30)
	obsRetentionDays := envInt("OBSERVATION_RETENTION_DAYS", 30)

	store, err := db.Open(ctx, dsn, time.Duration(cooldownSec)*time.Second, ipHourlyLimit)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer store.Close()

	pub := publish.New(store, blobDir)
	wake := make(chan struct{}, 1)
	notify := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}

	// Background publisher loop: debounced 5s on upload wake + periodic safety sweep.
	go func() {
		if err := pub.RunOnce(ctx); err != nil {
			log.Printf("initial publish: %v", err)
		}
		safety := time.NewTicker(time.Duration(publishSec) * time.Second)
		defer safety.Stop()
		var debounce <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				if debounce == nil {
					debounce = time.After(5 * time.Second)
				}
			case <-debounce:
				debounce = nil
				if err := pub.RunOnce(ctx); err != nil {
					log.Printf("publish error: %v", err)
				}
			case <-safety.C:
				if err := pub.RunOnce(ctx); err != nil {
					log.Printf("publish error: %v", err)
				}
			}
		}
	}()

	// Daily retention job: prune raw observations > 30d and old blob revisions > 48h.
	go func() {
		tick := time.NewTicker(12 * time.Hour)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if n, err := store.PruneOldObservations(ctx, obsRetentionDays); err != nil {
					log.Printf("prune observations: %v", err)
				} else if n > 0 {
					log.Printf("pruned %d old raw observations", n)
				}
				if err := pub.CleanupOldBlobs(ctx, 48*time.Hour); err != nil {
					log.Printf("cleanup blobs: %v", err)
				}
			}
		}
	}()

	srv := api.New(store, blobDir, cooldownSec, notify)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Printf("ahsync listening on %s (blobs=%s)", addr, blobDir)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}
