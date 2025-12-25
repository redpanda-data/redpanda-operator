// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package watcher

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultWatchInterval = 10 * time.Second

type caWatcher struct {
	sync.RWMutex

	currentCertPool *x509.CertPool
	watcher         *fsnotify.Watcher
	interval        time.Duration
	log             logr.Logger

	caPath string

	cachedCA []byte

	// callback is a function to be invoked when the certificate changes.
	callback func(*x509.CertPool)
}

func newCAWatcher(caPath string) (*caWatcher, error) {
	var err error

	cw := &caWatcher{
		caPath:   caPath,
		interval: defaultWatchInterval,
	}

	// Initial read of certificate and key.
	if err := cw.readCertificate(); err != nil {
		return nil, err
	}

	cw.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return cw, nil
}

func (cw *caWatcher) getCA() (*x509.CertPool, error) {
	cw.RLock()
	defer cw.RUnlock()
	return cw.currentCertPool, nil
}

func (cw *caWatcher) start(ctx context.Context) error {
	cw.log = ctrl.LoggerFrom(ctx).WithName("CAWatcher")

	files := sets.New(cw.caPath)

	{
		var watchErr error
		if err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Second, true, func(ctx context.Context) (done bool, err error) {
			for _, f := range files.UnsortedList() {
				if err := cw.watcher.Add(f); err != nil {
					watchErr = err
					return false, nil //nolint:nilerr // We want to keep trying.
				}
				// We've added the watch, remove it from the set.
				files.Delete(f)
			}
			return true, nil
		}); err != nil {
			return fmt.Errorf("failed to add watches: %w", kerrors.NewAggregate([]error{err, watchErr}))
		}
	}

	go cw.watch()

	ticker := time.NewTicker(cw.interval)
	defer ticker.Stop()

	cw.log.Info("Starting certificate poll+watcher", "interval", cw.interval)
	for {
		select {
		case <-ctx.Done():
			return cw.watcher.Close()
		case <-ticker.C:
			if err := cw.readCertificate(); err != nil {
				cw.log.Error(err, "failed read certificate")
			}
		}
	}
}

func (cw *caWatcher) watch() {
	for {
		select {
		case event, ok := <-cw.watcher.Events:
			// Channel is closed.
			if !ok {
				return
			}

			cw.handleEvent(event)
		case err, ok := <-cw.watcher.Errors:
			// Channel is closed.
			if !ok {
				return
			}

			cw.log.Error(err, "certificate watch error")
		}
	}
}

func (cw *caWatcher) updateCachedCertPool(pool *x509.CertPool, caPEMBlock []byte) bool {
	cw.Lock()
	defer cw.Unlock()

	if cw.currentCertPool != nil &&
		bytes.Equal(cw.cachedCA, caPEMBlock) {
		cw.log.V(7).Info("certificate already cached")
		return false
	}
	cw.currentCertPool = pool
	cw.cachedCA = caPEMBlock
	return true
}

func (cw *caWatcher) readCertificate() error {
	caPEMBlock, err := os.ReadFile(cw.caPath)
	if err != nil {
		return err
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEMBlock) {
		return fmt.Errorf("unable to append the CA certificate to CA pool")
	}

	if !cw.updateCachedCertPool(caPool, caPEMBlock) {
		return nil
	}

	cw.log.Info("Updated current CA")

	cw.RLock()
	defer cw.RUnlock()
	if cw.callback != nil {
		go func() {
			cw.callback(caPool)
		}()
	}
	return nil
}

func (cw *caWatcher) handleEvent(event fsnotify.Event) {
	// Only care about events which may modify the contents of the file.
	switch {
	case event.Op.Has(fsnotify.Write):
	case event.Op.Has(fsnotify.Create):
	case event.Op.Has(fsnotify.Chmod), event.Op.Has(fsnotify.Remove):
		// If the file was removed or renamed, re-add the watch to the previous name
		if err := cw.watcher.Add(event.Name); err != nil {
			cw.log.Error(err, "error re-watching file")
		}
	default:
		return
	}

	cw.log.V(1).Info("certificate event", "event", event)
	if err := cw.readCertificate(); err != nil {
		cw.log.Error(err, "error re-reading certificate")
	}
}
