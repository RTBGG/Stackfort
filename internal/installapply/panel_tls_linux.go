// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/paneltls"
)

const panelTLSRenewalWindow = 30 * 24 * time.Hour

func reconcilePanelTLS(now time.Time) (bool, error) {
	content, exists, info, err := readExistingFile(nginxbaseline.PanelTLSBundlePath)
	if err != nil {
		return false, err
	}
	if exists {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 || stat.Uid != 0 || stat.Gid != 0 {
			return false, errors.New("panel TLS bundle has unsafe ownership or links")
		}
		certificate, validateErr := paneltls.Validate(content, now)
		if validateErr != nil {
			return false, errors.New("panel TLS bundle conflicts with managed state")
		}
		if certificate.NotAfter.After(now.UTC().Add(panelTLSRenewalWindow)) {
			if info.Mode().Perm() == 0o600 {
				return false, nil
			}
			return true, os.Chmod(nginxbaseline.PanelTLSBundlePath, 0o600)
		}
	}
	hostname, _ := os.Hostname()
	bundle, err := paneltls.New(now, hostname, panelAddresses(), nil)
	if err != nil {
		return false, fmt.Errorf("generate panel TLS bundle: %w", err)
	}
	defer clear(bundle)
	if err := atomicWriteFile(nginxbaseline.PanelTLSBundlePath, bundle, 0, 0, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func verifyPanelTLS(now time.Time) (*x509.Certificate, error) {
	content, exists, info, err := readExistingFile(nginxbaseline.PanelTLSBundlePath)
	if err != nil || !exists || info.Mode().Perm() != 0o600 {
		return nil, errors.New("panel TLS bundle is unavailable or has unsafe mode")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != 0 || stat.Gid != 0 {
		return nil, errors.New("panel TLS bundle has unsafe ownership or links")
	}
	certificate, err := paneltls.Validate(content, now)
	if err != nil || !certificate.NotAfter.After(now.UTC().Add(panelTLSRenewalWindow)) {
		return nil, errors.New("panel TLS bundle is invalid or requires renewal")
	}
	return certificate, nil
}

func panelAddresses() []net.IP {
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	addresses := make([]net.IP, 0, len(interfaces))
	for _, value := range interfaces {
		address, _, err := net.ParseCIDR(value.String())
		if err == nil && address != nil && !address.IsUnspecified() {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

func panelHTTPClient(now time.Time) (*http.Client, error) {
	certificate, err := verifyPanelTLS(now)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	return &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    pool,
			ServerName: "127.0.0.1",
		}},
	}, nil
}
