package server

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// certCheckInterval is how often the certificate files are checked for a
// renewal, at most.
const certCheckInterval = 30 * time.Second

// certificate serves a certificate from its files, and loads them again
// when they change, so that a renewed certificate needs no restart.
type certificate struct {
	certFile, keyFile string
	logger            *slog.Logger
	now               func() time.Time

	mu       sync.Mutex
	cert     *tls.Certificate
	modified time.Time // of the newer file, when the certificate was loaded
	checked  time.Time
}

func loadCertificate(certFile, keyFile string, logger *slog.Logger) (*certificate, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("set both the certificate and the key file for TLS")
	}
	c := &certificate{certFile: certFile, keyFile: keyFile, logger: logger, now: time.Now}
	modified, err := c.modTime()
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load the TLS certificate: %w", err)
	}
	c.cert, c.modified, c.checked = &cert, modified, c.now()
	return c, nil
}

// modTime returns when the newer of the two files changed.
func (c *certificate) modTime() (time.Time, error) {
	var latest time.Time
	for _, f := range []string{c.certFile, c.keyFile} {
		info, err := os.Stat(f)
		if err != nil {
			return time.Time{}, fmt.Errorf("TLS certificate: %w", err)
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest, nil
}

// get is the tls.Config GetCertificate function. A certificate that fails
// to load keeps the old one in service, with a warning.
func (c *certificate) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now := c.now(); now.Sub(c.checked) >= certCheckInterval {
		c.checked = now
		modified, err := c.modTime()
		if err == nil && modified.After(c.modified) {
			var cert tls.Certificate
			if cert, err = tls.LoadX509KeyPair(c.certFile, c.keyFile); err == nil {
				c.cert, c.modified = &cert, modified
				c.logger.Info("loaded the renewed TLS certificate", "file", c.certFile)
			}
		}
		if err != nil {
			c.logger.Warn("TLS certificate can't be loaded; serving the one loaded before", "error", err)
		}
	}
	return c.cert, nil
}
