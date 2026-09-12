package serve

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A configured certificate is actually served, over TLS, speaking HTTP/2.
//
// Exercised against a real listener rather than read out of the server struct,
// because a TLSConfig that is populated and never used looks identical from
// inside the process — and that is the failure this is for: a deployment that
// believes it is encrypted.
func TestAConfiguredCertificateIsActuallyServed(t *testing.T) {
	cert, key, pool := certificateFor(t, "localhost")
	t.Setenv(CertEnv, cert)
	t.Setenv(KeyEnv, key)

	srv := &http.Server{
		Addr: freeAddr(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, r.Proto)
		}),
	}
	t.Cleanup(func() { _ = srv.Close() })

	var announced string
	go func() { _ = Run(srv, func(what string) { announced = what }) }()
	waitFor(t, srv.Addr)

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2: true,
	}}
	resp, err := client.Get("https://" + srv.Addr + "/")
	if err != nil {
		t.Fatalf("the server did not answer over TLS: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// Every service speaks HTTP/2 in plaintext through the h2c wrapper, which
	// only handles the cleartext upgrade. Without ALPN configured the server
	// negotiates HTTP/1.1 and the protocol quietly changes on the day somebody
	// turns TLS on.
	if got := string(body); got != "HTTP/2.0" {
		t.Errorf("the TLS listener negotiated %s, not HTTP/2", got)
	}
	if !strings.Contains(announced, "https://") {
		t.Errorf("the startup line is %q and does not say the connection is encrypted", announced)
	}
}

// Half a TLS configuration is a refusal, not a plaintext listener.
//
// This is the case worth refusing over. A deployment that sets a certificate
// path and mistypes the key variable has said what it wants; serving plaintext
// and carrying on answers every request correctly, logs nothing anybody reads,
// and is a platform that believes it is encrypted.
func TestHalfATLSConfigurationIsRefused(t *testing.T) {
	cert, key, _ := certificateFor(t, "localhost")

	for _, c := range []struct{ name, cert, key string }{
		{"certificate without key", cert, ""},
		{"key without certificate", "", key},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(CertEnv, c.cert)
			t.Setenv(KeyEnv, c.key)

			srv := &http.Server{Addr: freeAddr(t), Handler: http.NotFoundHandler()}
			t.Cleanup(func() { _ = srv.Close() })

			// In a goroutine with a deadline, because the failure being
			// checked for is a fallback to ListenAndServe — which does not
			// return a wrong answer, it never returns at all. A synchronous
			// call would hang rather than fail, and a test that hangs reports
			// nothing about what went wrong.
			served := make(chan string, 1)
			failed := make(chan error, 1)
			go func() { failed <- Run(srv, func(what string) { served <- what }) }()

			select {
			case err := <-failed:
				if err == nil {
					t.Fatal("a half-configured server started")
				}
				for _, want := range []string{CertEnv, KeyEnv} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not mention %s: %v", want, err)
					}
				}
			case what := <-served:
				t.Fatalf("a half-configured server is serving: %q", what)
			case <-time.After(10 * time.Second):
				t.Fatal("a half-configured server neither refused nor announced itself")
			}
		})
	}
}

// A certificate that cannot be read is named in the refusal.
func TestAnUnreadableCertificateIsNamed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere.crt")
	t.Setenv(CertEnv, missing)
	t.Setenv(KeyEnv, filepath.Join(t.TempDir(), "nowhere.key"))

	srv := &http.Server{Addr: freeAddr(t), Handler: http.NotFoundHandler()}
	t.Cleanup(func() { _ = srv.Close() })

	err := Run(srv, func(string) {})
	if err == nil {
		t.Fatal("a server with no readable certificate started")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the refusal does not name the file: %v", err)
	}
}

// A deployment can require TLS and have the process refuse to start without it.
//
// Because a warning at startup is read once, by the person who deployed it, on
// the day they deployed it.
func TestADeploymentCanRequireTLS(t *testing.T) {
	t.Setenv(CertEnv, "")
	t.Setenv(KeyEnv, "")

	for _, c := range []struct {
		value   string
		refuses bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"1", true},
		{"true", true},
		{"yes", true},
		// Anything unexpected reads as the stricter answer: the mistake that
		// matters is a required TLS silently becoming optional.
		{"maybe", true},
	} {
		t.Run("require="+c.value, func(t *testing.T) {
			t.Setenv(RequireEnv, c.value)

			srv := &http.Server{Addr: freeAddr(t), Handler: http.NotFoundHandler()}
			t.Cleanup(func() { _ = srv.Close() })

			if !c.refuses {
				// It would serve, so only check that it does not refuse before
				// listening — running it would block.
				if required() {
					t.Errorf("%s=%q was read as requiring TLS", RequireEnv, c.value)
				}
				return
			}
			// Same reason as above: the failure is a server that serves.
			served := make(chan string, 1)
			failed := make(chan error, 1)
			go func() { failed <- Run(srv, func(what string) { served <- what }) }()
			select {
			case err := <-failed:
				if err == nil {
					t.Fatalf("%s=%q and the process served plaintext anyway", RequireEnv, c.value)
				}
			case what := <-served:
				t.Fatalf("%s=%q and the process is serving: %q", RequireEnv, c.value, what)
			case <-time.After(10 * time.Second):
				t.Fatalf("%s=%q and the process neither refused nor announced itself",
					RequireEnv, c.value)
			}
		})
	}
}

// With nothing configured it serves plaintext and says so in the startup line.
//
// Plaintext is allowed: a cluster terminates TLS at the ingress, and a service
// that refused to start without its own certificate would be unrunnable in the
// commonest arrangement there is. What is not allowed is not knowing.
func TestPlaintextIsAnnouncedAsPlaintext(t *testing.T) {
	t.Setenv(CertEnv, "")
	t.Setenv(KeyEnv, "")
	t.Setenv(RequireEnv, "")

	srv := &http.Server{Addr: freeAddr(t), Handler: http.NotFoundHandler()}
	t.Cleanup(func() { _ = srv.Close() })

	announced := make(chan string, 1)
	go func() { _ = Run(srv, func(what string) { announced <- what }) }()

	select {
	case what := <-announced:
		if !strings.Contains(what, "NOT encrypted") {
			t.Errorf("the startup line is %q and a reader cannot tell this is plaintext", what)
		}
		if !strings.Contains(what, CertEnv) {
			t.Errorf("the startup line does not say what to set: %q", what)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server never announced itself")
	}
}

// No main function calls ListenAndServe for itself.
//
// All thirty did, and none of them served TLS. A main that goes back to it is a
// process that cannot be given a certificate at all, and nothing about its own
// behaviour would show it — it answers every request exactly as before.
//
// RUN WITH -count=1. It reads Go files in other modules, which the test cache
// does not track.
func TestNoMainListensForItself(t *testing.T) {
	root := repoRoot(t)
	mains, err := filepath.Glob(filepath.Join(root, "services", "*", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mains) < 20 {
		t.Fatalf("found only %d main functions; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(mains))
	}

	var direct []string
	for _, path := range mains {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(src), "srv.ListenAndServe(") ||
			strings.Contains(string(src), "srv.ListenAndServeTLS(") {
			direct = append(direct, filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))))
		}
	}
	if len(direct) > 0 {
		t.Errorf("%d main functions listen for themselves instead of calling serve.Run:\n  %s\n"+
			"Those cannot be given a certificate, and they answer every request "+
			"exactly as they did before.",
			len(direct), strings.Join(direct, "\n  "))
	}
	t.Logf("checked %d main functions", len(mains))
}

// certificateFor writes a self-signed certificate and returns the two paths and
// a pool that trusts it.
func certificateFor(t *testing.T, host string) (certPath, keyPath string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{host},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	pool = x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return certPath, keyPath, pool
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func waitFor(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing is listening on %s", addr)
}
