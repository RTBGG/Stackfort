// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/RTBGG/stackfort/internal/panelconfig"
)

const (
	panelACMEDirectory   = "https://acme-v02.api.letsencrypt.org/directory"
	panelACMEAccountPath = panelconfig.TLSDirectory + "/acme-account.pem"
	panelACMEConsentPath = panelconfig.TLSDirectory + "/acme-consent.json"
	panelACMEAttemptPath = panelconfig.TLSDirectory + "/acme-attempt.json"
)

type panelIssueFunc func(context.Context, string, string, crypto.Signer, operations.ACMEIssueCallbacks) ([]byte, []byte, error)

type panelACMEConsent struct {
	Email         string `json:"email"`
	TermsAccepted bool   `json:"termsAccepted"`
}

func (manager *panelManager) acme(ctx context.Context, workspace *linuxActivationWorkspace, request PanelRequest) (PanelStatus, error) {
	current, previous, err := manager.current()
	if err != nil {
		return PanelStatus{}, err
	}
	status, err := manager.status()
	if err != nil {
		return status, err
	}
	if request.Action == "renew" {
		if !current.AutoRenew || current.ChallengeOnly || current.Hostname == "" {
			return status, nil
		}
		request.Hostname = current.Hostname
	} else if panelconfig.Hostname(request.Hostname) != nil || panelconfig.Email(request.Email) != nil || !request.AcceptTerms {
		return status, errors.New("panel ACME requires a hostname, contact email and explicit terms acceptance")
	}
	if err := manager.checkSites(workspace, request.Hostname); err != nil {
		return status, err
	}
	if current.AutoRenew && current.Hostname == request.Hostname && status.Enabled {
		bundle, err := manager.read(current.BundlePath(), true)
		if err != nil {
			return status, err
		}
		defer clear(bundle)
		block, _ := pem.Decode(bundle)
		if block == nil {
			return status, panelconfig.ErrInvalid
		}
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return status, err
		}
		// Renew in the last third of the actual certificate lifetime, at most
		// 30 days before expiry. This also accommodates shorter-lived certs.
		window := min(30*24*time.Hour, leaf.NotAfter.Sub(leaf.NotBefore)/3)
		if leaf.NotAfter.After(time.Now().Add(window)) {
			return status, nil
		}
	}
	accountKey, email, err := manager.acmeAccount(request)
	if err != nil {
		return status, err
	}
	if err := manager.acmeCooldown(); err != nil {
		return status, err
	}
	challenge, err := panelconfig.Render(manager.spec, panelconfig.Config{Hostname: request.Hostname, AutoRenew: true, ChallengeOnly: true})
	if err != nil {
		return status, err
	}
	journal := panelJournal{SchemaVersion: 1, Previous: string(previous), Next: challenge}
	// Existing same-host panel configuration already serves HTTP-01; keep its
	// working HTTPS listener throughout renewals.
	if current.Hostname == request.Hostname && !current.ChallengeOnly {
		journal.Next = string(previous)
	}
	if err := manager.savePanelJournal(journal); err != nil {
		return status, err
	}
	if journal.Next != string(previous) {
		if err := manager.activatePanel(ctx, journal.Next); err != nil {
			return manager.rollbackPanel(err)
		}
	}
	issue := manager.issue
	if issue == nil {
		issue = issuePanelCertificate
	}
	certificate, key, err := issue(ctx, request.Hostname, email, accountKey, operations.ACMEIssueCallbacks{
		RecordOrderURL: func(context.Context, string) error { return nil },
		PresentHTTP01: func(_ context.Context, token, authorization string) error {
			if acmehttp01.ValidateKeyAuthorization(token, authorization) != nil || journal.Token != "" {
				return panelconfig.ErrInvalid
			}
			path := acmehttp01.ChallengeDirectory + "/" + token
			if _, err := manager.read(path, false); !errors.Is(err, os.ErrNotExist) {
				return ErrConflict
			}
			journal.Token = token
			if err := manager.savePanelJournal(journal); err != nil {
				return err
			}
			return manager.write(path, []byte(authorization), 0o644)
		},
		CleanupHTTP01: func(_ context.Context, token string) error {
			if acmehttp01.ValidateToken(token) != nil {
				return panelconfig.ErrInvalid
			}
			if journal.Token == "" {
				return nil
			}
			if journal.Token != token {
				return ErrConflict
			}
			if err := manager.remove(acmehttp01.ChallengeDirectory + "/" + token); err != nil {
				return err
			}
			journal.Token = ""
			return manager.savePanelJournal(journal)
		},
	})
	defer clear(key)
	if err != nil {
		return manager.rollbackPanel(errors.New("panel ACME issuance failed; verify DNS, public TCP/80 reachability and CA limits before retrying"))
	}
	config, bundle, err := panelconfig.Import(request.Hostname, certificate, key, time.Now(), manager.roots)
	if err != nil {
		return manager.rollbackPanel(err)
	}
	defer clear(bundle)
	config.AutoRenew = true
	if err := manager.storeBundle(config, bundle); err != nil {
		return manager.rollbackPanel(err)
	}
	next, err := panelconfig.Render(manager.spec, config)
	if err != nil {
		return manager.rollbackPanel(err)
	}
	journal.Intermediate, journal.Next = journal.Next, next
	if err := manager.savePanelJournal(journal); err != nil {
		return manager.rollbackPanel(err)
	}
	if err := manager.activatePanel(ctx, next); err != nil {
		return manager.rollbackPanel(err)
	}
	if err := manager.health(ctx, config, bundle); err != nil {
		return manager.rollbackPanel(err)
	}
	if journal.Token != "" {
		return manager.rollbackPanel(errors.New("ACME challenge cleanup is incomplete"))
	}
	if err := manager.remove(panelconfig.JournalPath); err != nil {
		return PanelStatus{}, err
	}
	return manager.status()
}

func (manager *panelManager) acmeAccount(request PanelRequest) (crypto.Signer, string, error) {
	var consent panelACMEConsent
	content, err := manager.read(panelACMEConsentPath, true)
	if err == nil {
		if decodeStrictJSON(content, &consent) != nil || !consent.TermsAccepted || panelconfig.Email(consent.Email) != nil {
			return nil, "", ErrConflict
		}
		if request.Action == "issue" && request.Email != consent.Email {
			return nil, "", errors.New("panel ACME contact differs from the existing root-managed account")
		}
	} else if errors.Is(err, os.ErrNotExist) && request.Action == "issue" && request.AcceptTerms && panelconfig.Email(request.Email) == nil {
		consent = panelACMEConsent{Email: request.Email, TermsAccepted: true}
		encoded, _ := json.Marshal(consent)
		if err := manager.write(panelACMEConsentPath, encoded, 0o600); err != nil {
			return nil, "", err
		}
	} else {
		return nil, "", ErrConflict
	}
	encoded, err := manager.read(panelACMEAccountPath, true)
	if errors.Is(err, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, "", err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, "", err
		}
		defer clear(der)
		encoded = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		defer clear(encoded)
		if err := manager.write(panelACMEAccountPath, encoded, 0o600); err != nil {
			return nil, "", err
		}
		return key, consent.Email, nil
	}
	if err != nil {
		return nil, "", err
	}
	defer clear(encoded)
	block, rest := pem.Decode(encoded)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, "", ErrConflict
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, "", ErrConflict
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, "", ErrConflict
	}
	return key, consent.Email, nil
}

func (manager *panelManager) acmeCooldown() error {
	var attempt struct {
		At time.Time `json:"at"`
	}
	content, err := manager.read(panelACMEAttemptPath, true)
	if err == nil {
		if decodeStrictJSON(content, &attempt) != nil || attempt.At.IsZero() {
			return ErrConflict
		}
		if time.Now().Before(attempt.At.Add(time.Hour)) {
			return errors.New("panel ACME issuance cooldown: wait at least one hour after the last attempt")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	attempt.At = time.Now().UTC()
	content, _ = json.Marshal(attempt)
	return manager.write(panelACMEAttemptPath, content, 0o600)
}

// Fixed authority, no environment proxy, no redirects, bounded CA responses.
// The ACME library follows order/account URLs only through this transport.
type panelACMETransport struct{ transport http.RoundTripper }

func (transport panelACMETransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "acme-v02.api.letsencrypt.org" || request.URL.User != nil {
		return nil, errors.New("unapproved panel ACME endpoint")
	}
	response, err := transport.transport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &boundedPanelACMEBody{ReadCloser: response.Body, remaining: 2 << 20}
	return response, nil
}

type boundedPanelACMEBody struct {
	io.ReadCloser
	remaining int64
}

func (body *boundedPanelACMEBody) Read(buffer []byte) (int, error) {
	if body.remaining <= 0 {
		return 0, errors.New("panel ACME response exceeded limit")
	}
	if int64(len(buffer)) > body.remaining {
		buffer = buffer[:body.remaining]
	}
	count, err := body.ReadCloser.Read(buffer)
	body.remaining -= int64(count)
	return count, err
}

func issuePanelCertificate(ctx context.Context, hostname, email string, account crypto.Signer, callbacks operations.ACMEIssueCallbacks) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: panelACMETransport{transport}, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	registration, err := (operations.RFC8555Registrar{HTTPClient: client}).Register(ctx, operations.ACMERegistrationRequest{
		DirectoryURL: panelACMEDirectory, ContactEmail: email, TermsAccepted: true, Signer: account,
	})
	if err != nil {
		return nil, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	result, err := (operations.RFC8555Issuer{HTTPClient: client}).Issue(ctx, operations.ACMEIssueRequest{
		DirectoryURL: panelACMEDirectory, AccountURI: registration.AccountURI, AccountSigner: account, CertificateKey: key, Names: []string{hostname},
	}, callbacks)
	if err != nil {
		return nil, nil, err
	}
	var chain []byte
	for _, der := range result.DERChain {
		chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	defer clear(der)
	return chain, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
