// Package upgradetest provides a fake Sigstore (Fulcio CA and Rekor log) and
// a fake GitHub release server for tests. Test-only.
package upgradetest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
)

// ReleaseSubject is the certificate identity of the real release workflow.
const ReleaseSubject = "https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/tags/v9.9.9"

// GitHubIssuer is the GitHub Actions OIDC issuer.
const GitHubIssuer = "https://token.actions.githubusercontent.com"

// Sigstore is an in-memory Fulcio CA and Rekor log.
type Sigstore struct {
	t        *testing.T
	rootKey  *ecdsa.PrivateKey
	rootCert *x509.Certificate
	interKey *ecdsa.PrivateKey
	inter    *x509.Certificate
	logKey   *ecdsa.PrivateKey
	logID    []byte
	Now      time.Time
}

// NewSigstore returns a fake Sigstore whose clock is the current time.
func NewSigstore(t *testing.T) *Sigstore {
	t.Helper()
	s := &Sigstore{t: t, Now: time.Now().Truncate(time.Second)}
	s.rootKey, s.rootCert = s.ca("fake-sigstore", nil, nil)
	s.interKey, s.inter = s.ca("fake-sigstore-intermediate", s.rootCert, s.rootKey)
	s.logKey = s.key()
	der, err := x509.MarshalPKIXPublicKey(&s.logKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	id := sha256.Sum256(der)
	s.logID = id[:]
	return s
}

func (s *Sigstore) key() *ecdsa.PrivateKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		s.t.Fatal(err)
	}
	return k
}

func (s *Sigstore) ca(name string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*ecdsa.PrivateKey, *x509.Certificate) {
	k := s.key()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name, Organization: []string{"sigstore.dev"}},
		NotBefore:             s.Now.Add(-24 * time.Hour),
		NotAfter:              s.Now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if parent == nil {
		parent, parentKey = tmpl, k
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &k.PublicKey, parentKey)
	if err != nil {
		s.t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		s.t.Fatal(err)
	}
	return k, c
}

// TrustedRoot returns the trusted_root.json document for this Sigstore.
func (s *Sigstore) TrustedRoot() []byte {
	der, _ := x509.MarshalPKIXPublicKey(&s.logKey.PublicKey)
	start := s.Now.Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	doc := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.trustedroot+json;version=0.1",
		"tlogs": []any{map[string]any{
			"baseUrl":       "https://rekor.example",
			"hashAlgorithm": "SHA2_256",
			"publicKey": map[string]any{
				"rawBytes":   base64.StdEncoding.EncodeToString(der),
				"keyDetails": "PKIX_ECDSA_P256_SHA_256",
				"validFor":   map[string]any{"start": start},
			},
			"logId": map[string]any{"keyId": base64.StdEncoding.EncodeToString(s.logID)},
		}},
		"certificateAuthorities": []any{map[string]any{
			"uri": "https://fulcio.example",
			"certChain": map[string]any{"certificates": []any{
				map[string]any{"rawBytes": base64.StdEncoding.EncodeToString(s.inter.Raw)},
				map[string]any{"rawBytes": base64.StdEncoding.EncodeToString(s.rootCert.Raw)},
			}},
			"validFor": map[string]any{"start": start},
		}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		s.t.Fatal(err)
	}
	return b
}

// Root returns the parsed trusted root for this Sigstore.
func (s *Sigstore) Root() *upgrade.TrustedRoot {
	r, err := upgrade.ParseTrustedRoot(s.TrustedRoot())
	if err != nil {
		s.t.Fatal(err)
	}
	return r
}

// Sign returns a cosign-style keyless bundle (v0.3) signing artifact as the
// workflow identity subject, issued by issuer.
func (s *Sigstore) Sign(artifact []byte, subject, issuer string) []byte {
	s.t.Helper()
	leafKey := s.key()
	u, err := url.Parse(subject)
	if err != nil {
		s.t.Fatal(err)
	}
	issuerV2, err := asn1.MarshalWithParams(issuer, "utf8")
	if err != nil {
		s.t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    s.Now.Add(-time.Minute),
		NotAfter:     s.Now.Add(9 * time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs:         []*url.URL{u},
		ExtraExtensions: []pkix.Extension{
			{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}, Value: []byte(issuer)},
			{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}, Value: issuerV2},
		},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, s.inter, &leafKey.PublicKey, s.interKey)
	if err != nil {
		s.t.Fatal(err)
	}
	digest := sha256.Sum256(artifact)
	sig, err := ecdsa.SignASN1(rand.Reader, leafKey, digest[:])
	if err != nil {
		s.t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"apiVersion": "0.0.1",
		"kind":       "hashedrekord",
		"spec": map[string]any{
			"data": map[string]any{"hash": map[string]any{"algorithm": "sha256", "value": hex.EncodeToString(digest[:])}},
			"signature": map[string]any{
				"content":   base64.StdEncoding.EncodeToString(sig),
				"publicKey": map[string]any{"content": base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))},
			},
		},
	})
	integrated, logIndex := s.Now.Unix(), int64(4242)
	payload, _ := json.Marshal(map[string]any{
		"body":           base64.StdEncoding.EncodeToString(body),
		"integratedTime": integrated,
		"logID":          hex.EncodeToString(s.logID),
		"logIndex":       logIndex,
	})
	ph := sha256.Sum256(payload)
	set, err := ecdsa.SignASN1(rand.Reader, s.logKey, ph[:])
	if err != nil {
		s.t.Fatal(err)
	}

	b, err := json.MarshalIndent(map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"certificate": map[string]any{"rawBytes": base64.StdEncoding.EncodeToString(der)},
			"tlogEntries": []any{map[string]any{
				"logIndex":          itoa(logIndex),
				"logId":             map[string]any{"keyId": base64.StdEncoding.EncodeToString(s.logID)},
				"kindVersion":       map[string]any{"kind": "hashedrekord", "version": "0.0.1"},
				"integratedTime":    itoa(integrated),
				"inclusionPromise":  map[string]any{"signedEntryTimestamp": base64.StdEncoding.EncodeToString(set)},
				"canonicalizedBody": base64.StdEncoding.EncodeToString(body),
			}},
			"timestampVerificationData": map[string]any{},
		},
		"messageSignature": map[string]any{
			"messageDigest": map[string]any{"algorithm": "SHA2_256", "digest": base64.StdEncoding.EncodeToString(digest[:])},
			"signature":     base64.StdEncoding.EncodeToString(sig),
		},
	}, "", "  ")
	if err != nil {
		s.t.Fatal(err)
	}
	return b
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
