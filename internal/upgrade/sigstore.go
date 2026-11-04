package upgrade

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// This file verifies a Sigstore bundle made by `cosign sign-blob --bundle`
// (keyless: a short-lived Fulcio certificate plus a Rekor v1 log entry) with
// the standard library only. It checks what `cosign verify-blob` checks for
// such a bundle, except the certificate transparency SCT:
//
//   - the Rekor entry's signed entry timestamp (SET) verifies with a trusted
//     log key, and the entry records this signature, certificate and digest;
//   - the certificate chains to a trusted Fulcio CA at the log's time;
//   - the certificate names the expected OIDC issuer and signer identity;
//   - the signature verifies over the artifact with the certificate's key.

// publicGoodRoot is Sigstore's public-good trusted root (Fulcio CAs and the
// Rekor v1 key), as published in its TUF repository (trusted_root.json).
//
//go:embed trusted_root.json
var publicGoodRoot []byte

// TrustedRoot holds the Fulcio CAs and Rekor logs signatures must chain to.
type TrustedRoot struct {
	CAs  []CertificateAuthority
	Logs map[string]TransparencyLog // by hex log ID (SHA-256 of the DER key)
}

// CertificateAuthority is a Fulcio CA and the period it issued certificates.
type CertificateAuthority struct {
	Root          *x509.Certificate
	Intermediates []*x509.Certificate
	Start, End    time.Time // End zero means still active
}

// TransparencyLog is a Rekor log key and the period it signed entries.
type TransparencyLog struct {
	Key        *ecdsa.PublicKey
	Start, End time.Time
}

// Identity is the signer a bundle's certificate must name.
type Identity struct {
	Issuer  string         // OIDC issuer, matched exactly
	Subject *regexp.Regexp // certificate SAN (for GitHub Actions, the workflow URI)
}

// ReleaseIdentity is the release workflow of this repository on GitHub
// Actions: a tag push signs with refs/tags/vX.Y.Z, a manual rerun with the
// branch it was dispatched from, main.
var ReleaseIdentity = Identity{
	Issuer: "https://token.actions.githubusercontent.com",
	Subject: regexp.MustCompile(`^https://github\.com/BrainerVirus/youtrack-cli/\.github/workflows/release\.yml@refs/` +
		`(tags/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?|heads/main)$`),
}

// PublicGoodRoot returns the embedded Sigstore public-good trusted root.
func PublicGoodRoot() (*TrustedRoot, error) { return ParseTrustedRoot(publicGoodRoot) }

type validFor struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func (v validFor) times() (start, end time.Time, err error) {
	if v.Start == "" {
		return start, end, errors.New("validity period has no start")
	}
	if start, err = time.Parse(time.RFC3339, v.Start); err != nil {
		return start, end, err
	}
	if v.End != "" {
		end, err = time.Parse(time.RFC3339, v.End)
	}
	return start, end, err
}

func within(t, start, end time.Time) bool {
	return !t.Before(start) && (end.IsZero() || !t.After(end))
}

// ParseTrustedRoot reads a Sigstore trusted_root.json document.
func ParseTrustedRoot(data []byte) (*TrustedRoot, error) {
	var doc struct {
		Tlogs []struct {
			PublicKey struct {
				RawBytes   []byte   `json:"rawBytes"`
				KeyDetails string   `json:"keyDetails"`
				ValidFor   validFor `json:"validFor"`
			} `json:"publicKey"`
		} `json:"tlogs"`
		CertificateAuthorities []struct {
			CertChain struct {
				Certificates []struct {
					RawBytes []byte `json:"rawBytes"`
				} `json:"certificates"`
			} `json:"certChain"`
			ValidFor validFor `json:"validFor"`
		} `json:"certificateAuthorities"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("trusted root: %w", err)
	}
	tr := &TrustedRoot{Logs: map[string]TransparencyLog{}}
	for _, l := range doc.Tlogs {
		if l.PublicKey.KeyDetails != "PKIX_ECDSA_P256_SHA_256" {
			continue // not a Rekor v1 key this verifier uses
		}
		pub, err := x509.ParsePKIXPublicKey(l.PublicKey.RawBytes)
		if err != nil {
			return nil, fmt.Errorf("trusted root: log key: %w", err)
		}
		key, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return nil, errors.New("trusted root: log key is not ECDSA")
		}
		start, end, err := l.PublicKey.ValidFor.times()
		if err != nil {
			return nil, fmt.Errorf("trusted root: log key: %w", err)
		}
		id := sha256.Sum256(l.PublicKey.RawBytes)
		tr.Logs[hex.EncodeToString(id[:])] = TransparencyLog{Key: key, Start: start, End: end}
	}
	for _, ca := range doc.CertificateAuthorities {
		certs := ca.CertChain.Certificates
		if len(certs) == 0 {
			return nil, errors.New("trusted root: CA without certificates")
		}
		parsed := make([]*x509.Certificate, 0, len(certs))
		for _, c := range certs {
			cert, err := x509.ParseCertificate(c.RawBytes)
			if err != nil {
				return nil, fmt.Errorf("trusted root: CA certificate: %w", err)
			}
			parsed = append(parsed, cert)
		}
		start, end, err := ca.ValidFor.times()
		if err != nil {
			return nil, fmt.Errorf("trusted root: CA: %w", err)
		}
		// The chain lists the issuing certificate first and the root last.
		tr.CAs = append(tr.CAs, CertificateAuthority{
			Root:          parsed[len(parsed)-1],
			Intermediates: parsed[:len(parsed)-1],
			Start:         start,
			End:           end,
		})
	}
	if len(tr.CAs) == 0 || len(tr.Logs) == 0 {
		return nil, errors.New("trusted root: no Fulcio CA or Rekor v1 log")
	}
	return tr, nil
}

// int64String decodes a protobuf-JSON int64, which may be a JSON string.
type int64String int64

func (n *int64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	v, err := strconv.ParseInt(s, 10, 64)
	*n = int64String(v)
	return err
}

type rawBytes struct {
	RawBytes []byte `json:"rawBytes"`
}

type tlogEntry struct {
	LogIndex int64String `json:"logIndex"`
	LogID    struct {
		KeyID []byte `json:"keyId"`
	} `json:"logId"`
	KindVersion struct {
		Kind    string `json:"kind"`
		Version string `json:"version"`
	} `json:"kindVersion"`
	IntegratedTime   int64String `json:"integratedTime"`
	InclusionPromise *struct {
		SignedEntryTimestamp []byte `json:"signedEntryTimestamp"`
	} `json:"inclusionPromise"`
	CanonicalizedBody []byte `json:"canonicalizedBody"`
}

type sigstoreBundle struct {
	MediaType            string `json:"mediaType"`
	VerificationMaterial struct {
		Certificate          *rawBytes `json:"certificate"`
		X509CertificateChain *struct {
			Certificates []rawBytes `json:"certificates"`
		} `json:"x509CertificateChain"`
		TlogEntries []tlogEntry `json:"tlogEntries"`
	} `json:"verificationMaterial"`
	MessageSignature *struct {
		MessageDigest *struct {
			Algorithm string `json:"algorithm"`
			Digest    []byte `json:"digest"`
		} `json:"messageDigest"`
		Signature []byte `json:"signature"`
	} `json:"messageSignature"`
}

// hashedRekord is the body of a Rekor v1 "hashedrekord" 0.0.1 entry.
type hashedRekord struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Data struct {
			Hash struct {
				Algorithm string `json:"algorithm"`
				Value     string `json:"value"`
			} `json:"hash"`
		} `json:"data"`
		Signature struct {
			Content   []byte `json:"content"`
			PublicKey struct {
				Content []byte `json:"content"`
			} `json:"publicKey"`
		} `json:"signature"`
	} `json:"spec"`
}

var (
	oidIssuerV1  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	oidIssuerV2  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}
	errSignature = errors.New("signature verification failed")
)

// VerifyBundle checks that bundleJSON is a valid keyless signature of
// artifact by id, against root. Any failed check is an error.
func VerifyBundle(bundleJSON, artifact []byte, root *TrustedRoot, id Identity) error {
	if root == nil || id.Subject == nil || id.Issuer == "" {
		return errors.New("signature verification is not configured")
	}
	var b sigstoreBundle
	if err := json.Unmarshal(bundleJSON, &b); err != nil {
		return fmt.Errorf("%w: invalid bundle: %w", errSignature, err)
	}
	if !strings.HasPrefix(b.MediaType, "application/vnd.dev.sigstore.bundle") {
		return fmt.Errorf("%w: not a Sigstore bundle", errSignature)
	}
	if b.MessageSignature == nil || len(b.MessageSignature.Signature) == 0 {
		return fmt.Errorf("%w: the bundle has no message signature", errSignature)
	}
	var certDER []byte
	switch vm := b.VerificationMaterial; {
	case vm.Certificate != nil:
		certDER = vm.Certificate.RawBytes
	case vm.X509CertificateChain != nil && len(vm.X509CertificateChain.Certificates) > 0:
		certDER = vm.X509CertificateChain.Certificates[0].RawBytes
	default:
		return fmt.Errorf("%w: the bundle has no certificate (keyless signature expected)", errSignature)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("%w: certificate: %w", errSignature, err)
	}
	digest := sha256.Sum256(artifact)
	sig := b.MessageSignature.Signature

	if md := b.MessageSignature.MessageDigest; md != nil {
		if md.Algorithm != "SHA2_256" || !bytes.Equal(md.Digest, digest[:]) {
			return fmt.Errorf("%w: the signed digest does not match the file", errSignature)
		}
	}

	// The transparency log gives the trusted signing time.
	if len(b.VerificationMaterial.TlogEntries) == 0 {
		return fmt.Errorf("%w: the bundle has no transparency log entry", errSignature)
	}
	var signedAt time.Time
	var logErr error
	for i := range b.VerificationMaterial.TlogEntries {
		t, err := verifyTlogEntry(&b.VerificationMaterial.TlogEntries[i], root, cert, sig, digest[:])
		if err == nil {
			signedAt, logErr = t, nil
			break
		}
		logErr = err
	}
	if logErr != nil {
		return fmt.Errorf("%w: transparency log: %w", errSignature, logErr)
	}

	if err := verifyChain(cert, root, signedAt); err != nil {
		return fmt.Errorf("%w: certificate: %w", errSignature, err)
	}
	if err := checkIdentity(cert, id); err != nil {
		return fmt.Errorf("%w: %w", errSignature, err)
	}

	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: unsupported key type %T", errSignature, cert.PublicKey)
	}
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return fmt.Errorf("%w: the signature does not match the file", errSignature)
	}
	return nil
}

func verifyTlogEntry(e *tlogEntry, root *TrustedRoot, cert *x509.Certificate, sig, digest []byte) (time.Time, error) {
	if e.KindVersion.Kind != "hashedrekord" || e.KindVersion.Version != "0.0.1" {
		return time.Time{}, fmt.Errorf("unsupported entry kind %s %s", e.KindVersion.Kind, e.KindVersion.Version)
	}
	if e.InclusionPromise == nil || len(e.InclusionPromise.SignedEntryTimestamp) == 0 {
		return time.Time{}, errors.New("entry has no signed entry timestamp (Rekor v1 expected)")
	}
	logID := hex.EncodeToString(e.LogID.KeyID)
	log, ok := root.Logs[logID]
	if !ok {
		return time.Time{}, errors.New("entry is from an unknown log")
	}
	integrated := time.Unix(int64(e.IntegratedTime), 0)
	if !within(integrated, log.Start, log.End) {
		return time.Time{}, errors.New("log key was not valid at the entry's time")
	}
	// The SET signs the RFC 8785 canonical JSON of these four fields; with
	// sorted keys, integers and base64/hex strings, encoding/json emits it.
	payload, err := json.Marshal(struct {
		Body           string `json:"body"`
		IntegratedTime int64  `json:"integratedTime"`
		LogID          string `json:"logID"`
		LogIndex       int64  `json:"logIndex"`
	}{
		Body:           base64.StdEncoding.EncodeToString(e.CanonicalizedBody),
		IntegratedTime: int64(e.IntegratedTime),
		LogID:          logID,
		LogIndex:       int64(e.LogIndex),
	})
	if err != nil {
		return time.Time{}, err
	}
	h := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(log.Key, h[:], e.InclusionPromise.SignedEntryTimestamp) {
		return time.Time{}, errors.New("signed entry timestamp does not verify")
	}

	var body hashedRekord
	if err := json.Unmarshal(e.CanonicalizedBody, &body); err != nil {
		return time.Time{}, fmt.Errorf("entry body: %w", err)
	}
	if body.Kind != "hashedrekord" || body.APIVersion != "0.0.1" {
		return time.Time{}, errors.New("entry body is not a hashedrekord 0.0.1")
	}
	if body.Spec.Data.Hash.Algorithm != "sha256" || !strings.EqualFold(body.Spec.Data.Hash.Value, hex.EncodeToString(digest)) {
		return time.Time{}, errors.New("entry records a different file digest")
	}
	if !bytes.Equal(body.Spec.Signature.Content, sig) {
		return time.Time{}, errors.New("entry records a different signature")
	}
	block, _ := pem.Decode(body.Spec.Signature.PublicKey.Content)
	if block == nil || !bytes.Equal(block.Bytes, cert.Raw) {
		return time.Time{}, errors.New("entry records a different certificate")
	}
	return integrated, nil
}

func verifyChain(cert *x509.Certificate, root *TrustedRoot, at time.Time) error {
	lastErr := errors.New("no Fulcio CA was active at the signing time")
	for _, ca := range root.CAs {
		if !within(at, ca.Start, ca.End) {
			continue
		}
		roots, inter := x509.NewCertPool(), x509.NewCertPool()
		roots.AddCert(ca.Root)
		for _, c := range ca.Intermediates {
			inter.AddCert(c)
		}
		_, err := cert.Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: inter,
			CurrentTime:   at,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		})
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func checkIdentity(cert *x509.Certificate, id Identity) error {
	issuer := ""
	for _, ext := range cert.Extensions {
		switch {
		case ext.Id.Equal(oidIssuerV2):
			var s string
			if _, err := asn1.UnmarshalWithParams(ext.Value, &s, "utf8"); err == nil {
				issuer = s
			}
		case ext.Id.Equal(oidIssuerV1) && issuer == "":
			issuer = string(ext.Value)
		}
	}
	if issuer != id.Issuer {
		return fmt.Errorf("certificate issuer %q is not %q", issuer, id.Issuer)
	}
	var sans []string
	for _, u := range cert.URIs {
		sans = append(sans, u.String())
	}
	sans = append(sans, cert.EmailAddresses...)
	for _, s := range sans {
		if id.Subject.MatchString(s) {
			return nil
		}
	}
	return fmt.Errorf("certificate identity %q is not this repository's release workflow", strings.Join(sans, ", "))
}
