package serving

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"k8s.io/klog"
)

func RunServer(ctx context.Context, staticDirPath string, ciConfigPath string) error {
	router := mux.NewRouter().StrictSlash(true)
	RegisterHandlers(router, staticDirPath, ciConfigPath)

	srv := &http.Server{
		Addr:    ":5000",
		Handler: router,
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	serverClosedChan := make(chan struct{})
	defer close(serverClosedChan)

	wg.Add(1)
	go func() {
		defer wg.Done()

		select {
		case <-ctx.Done():
			shutdownCtx, shutdownCtxCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer shutdownCtxCancel()
			klog.Info("Shutting down the server.")
			err := srv.Shutdown(shutdownCtx)
			if err != nil {
				klog.Fatal(err)
			}
		case <-serverClosedChan:
			break
		}
	}()

	klog.Infof("Starting listening on %q", srv.Addr)
	// TODO: wire TLS certs for HTTP2
	// err := srv.ListenAndServeTLS("cert/server.crt", "cert/server.key")
	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		klog.Fatal("ListenAndServe failed: %v", err)
	}

	return nil
}
