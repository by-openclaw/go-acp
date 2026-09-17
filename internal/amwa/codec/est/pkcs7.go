package est

// Minimal certs-only ("degenerate") PKCS#7 / CMS SignedData — the
// only PKCS#7 shape EST uses (RFC 7030 carries certificates, never
// signed content). The stdlib has no PKCS#7, and importing one for
// this would buy a dependency for two fixed ASN.1 shapes:
//
//	ContentInfo ::= SEQUENCE {
//	    contentType   OBJECT IDENTIFIER (signedData),
//	    content   [0] EXPLICIT SignedData }
//	SignedData ::= SEQUENCE {
//	    version           INTEGER,
//	    digestAlgorithms  SET OF ...   (empty),
//	    contentInfo       ContentInfo  (id-data, no content),
//	    certificates  [0] IMPLICIT SET OF Certificate,
//	    signerInfos       SET OF ...   (empty) }

import (
	"crypto/x509"
	"encoding/asn1"
	"fmt"
)

var (
	oidSignedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidData       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
)

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type signedData struct {
	Version          int
	DigestAlgorithms asn1.RawValue // SET, empty for certs-only
	ContentInfo      contentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"` // IMPLICIT [0]
	SignerInfos      asn1.RawValue // SET, empty for certs-only
}

// ParseCertsOnlyPKCS7 extracts the certificates from a DER SignedData.
func ParseCertsOnlyPKCS7(der []byte) ([]*x509.Certificate, error) {
	var ci contentInfo
	rest, err := asn1.Unmarshal(der, &ci)
	if err != nil {
		return nil, fmt.Errorf("est: pkcs7 ContentInfo: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("est: pkcs7: trailing bytes after ContentInfo")
	}
	if !ci.ContentType.Equal(oidSignedData) {
		return nil, fmt.Errorf("est: pkcs7 contentType %v is not signedData", ci.ContentType)
	}
	var sd signedData
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("est: pkcs7 SignedData: %w", err)
	}
	if len(sd.Certificates.Bytes) == 0 {
		return nil, fmt.Errorf("est: pkcs7: no certificates present")
	}
	// The [0] IMPLICIT wrapper holds concatenated Certificate DER
	// values; x509 parses them sequentially.
	certs, err := x509.ParseCertificates(sd.Certificates.Bytes)
	if err != nil {
		return nil, fmt.Errorf("est: pkcs7 certificates: %w", err)
	}
	return certs, nil
}

// EncodeCertsOnlyPKCS7 builds a DER certs-only SignedData.
func EncodeCertsOnlyPKCS7(certs []*x509.Certificate) ([]byte, error) {
	if len(certs) == 0 {
		return nil, fmt.Errorf("est: pkcs7: at least one certificate required")
	}
	var raw []byte
	for _, c := range certs {
		raw = append(raw, c.Raw...)
	}
	emptySet := asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true}
	sd := signedData{
		Version:          1,
		DigestAlgorithms: emptySet,
		ContentInfo:      contentInfo{ContentType: oidData},
		Certificates: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: raw,
		},
		SignerInfos: emptySet,
	}
	sdDER, err := asn1.Marshal(sd)
	if err != nil {
		return nil, fmt.Errorf("est: pkcs7 marshal SignedData: %w", err)
	}
	ci := contentInfo{
		ContentType: oidSignedData,
		Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: sdDER},
	}
	out, err := asn1.Marshal(ci)
	if err != nil {
		return nil, fmt.Errorf("est: pkcs7 marshal ContentInfo: %w", err)
	}
	return out, nil
}
