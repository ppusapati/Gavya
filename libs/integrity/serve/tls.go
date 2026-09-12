package serve

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/http2"
)

// Nothing in this platform ever served TLS.
//
// Every one of the thirty main functions called ListenAndServe, so sessions,
// passwords and a fortnight's payments crossed the network in the clear. Behind
// an ingress that terminates TLS that is a defensible arrangement; on a host
// running docker-compose, which is the shape this platform ships, there is no
// ingress and nothing was encrypted at all.
//
// # WHAT THIS DOES NOT DECIDE
//
// It does not insist on TLS here. A cluster deployment terminates at the
// ingress, and a service that refused to start without its own certificate
// would be unrunnable in the arrangement that is actually most common. What it
// refuses is not knowing: a deployment that means to serve TLS and is serving
// plaintext, and a deployment that cannot tell which it is doing.
//
// So: a certificate is configured or it is not, the choice is announced at
// startup in words, half a configuration is a refusal rather than a quiet
// fallback, and a deployment that requires encryption can say so and have the
// process refuse to start without it.
const (
	CertEnv = "GAVYA_TLS_CERT"
	KeyEnv  = "GAVYA_TLS_KEY"
	// RequireEnv makes plaintext a startup refusal rather than a warning.
	//
	// For the deployment that terminates TLS in this process and wants that
	// asserted rather than hoped for. A warning at startup is read once, by the
	// person who deployed it, on the day they deployed it.
	RequireEnv = "GAVYA_REQUIRE_TLS"
)

// Run serves, over TLS when a certificate is configured.
//
// announce is called exactly once, before serving, with what this is about to
// do. It takes the announcement rather than main composing its own because the
// two would drift: a log line saying https beside a process serving plaintext is
// worse than no log line, and it is the sort of difference nobody notices until
// somebody captures traffic.
func Run(srv *http.Server, announce func(string)) error {
	cert := strings.TrimSpace(os.Getenv(CertEnv))
	key := strings.TrimSpace(os.Getenv(KeyEnv))

	switch {
	case cert != "" && key != "":
		// Loaded here rather than left to ListenAndServeTLS so an unreadable or
		// mismatched pair is reported before the port is bound, naming the file.
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return fmt.Errorf("serve: reading the certificate %s and key %s: %w", cert, key, err)
		}
		if srv.TLSConfig == nil {
			srv.TLSConfig = &tls.Config{}
		}
		srv.TLSConfig.Certificates = []tls.Certificate{pair}
		// TLS 1.2 is the floor. Below it is not a weaker configuration, it is a
		// broken one.
		srv.TLSConfig.MinVersion = tls.VersionTLS12
		// HTTP/2 over TLS, explicitly.
		//
		// net/http already enables it for a TLS server on its own, and removing
		// this line was measured: the listener still negotiates h2. It stays
		// because that automatic path is conditional — it is skipped when
		// TLSNextProto is non-nil, and switched off by GODEBUG http2server=0 —
		// and every service in this platform speaks HTTP/2 in plaintext through
		// the h2c wrapper, which handles only the cleartext upgrade. Silently
		// dropping to HTTP/1.1 on the day somebody turns TLS on is a protocol
		// change nothing would report.
		if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		announce("listening on https://" + srv.Addr + ", certificate " + cert)
		return srv.ListenAndServeTLS("", "")

	case cert != "" || key != "":
		// Half a configuration. The dangerous reading is to serve plaintext and
		// carry on, which is a deployment that believes it is encrypted.
		missing, set := KeyEnv, CertEnv
		if key != "" {
			missing, set = CertEnv, KeyEnv
		}
		return fmt.Errorf("serve: %s is set and %s is not.\n"+
			"Serving plaintext from here would be a deployment that believes it is "+
			"encrypted. Set both, or neither", set, missing)

	case required():
		return fmt.Errorf("serve: %s is set and no certificate is configured.\n"+
			"Set %s and %s, or unset %s if TLS is terminated in front of this process",
			RequireEnv, CertEnv, KeyEnv, RequireEnv)

	default:
		announce("listening on http://" + srv.Addr + " — NOT encrypted. " +
			"Terminate TLS in front of this, or set " + CertEnv + " and " + KeyEnv + ". " +
			"Set " + RequireEnv + " to make this a refusal rather than this line")
		return srv.ListenAndServe()
	}
}

// required reports whether the deployment has said plaintext is unacceptable.
//
// Anything but the empty string and the usual spellings of no. Read that way
// round deliberately: a deployment that sets this to something unexpected gets
// the stricter behaviour, because the mistake that matters is a required TLS
// silently becoming optional.
func required() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(RequireEnv))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
