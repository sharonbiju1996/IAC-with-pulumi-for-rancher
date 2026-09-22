package main

import (
	"github.com/pulumi/pulumi-tls/sdk/v5/go/tls"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Certificates struct {
	CACertPEM     pulumi.StringOutput
	ServerCertPEM pulumi.StringOutput // leaf + CA chain
	ServerKeyPEM  pulumi.StringOutput
}

// newCertificates creates a private CA and a server certificate for Keycloak.
// Private keys live only in the (encrypted) Pulumi state and the cluster Secret.
func newCertificates(ctx *pulumi.Context, cfg Config) (*Certificates, error) {
	caKey, err := tls.NewPrivateKey(ctx, "ca-key", &tls.PrivateKeyArgs{
		Algorithm:  pulumi.String("ECDSA"),
		EcdsaCurve: pulumi.String("P256"),
	})
	if err != nil {
		return nil, err
	}
	caCert, err := tls.NewSelfSignedCert(ctx, "ca-cert", &tls.SelfSignedCertArgs{
		PrivateKeyPem:       caKey.PrivateKeyPem,
		IsCaCertificate:     pulumi.Bool(true),
		ValidityPeriodHours: pulumi.Int(24 * 365 * 5),
		AllowedUses:         pulumi.StringArray{pulumi.String("cert_signing"), pulumi.String("crl_signing")},
		Subject: &tls.SelfSignedCertSubjectArgs{
			CommonName:   pulumi.String("Keycloak Local Dev CA"),
			Organization: pulumi.String("keycloak-k8s-pulumi"),
		},
	})
	if err != nil {
		return nil, err
	}

	srvKey, err := tls.NewPrivateKey(ctx, "keycloak-key", &tls.PrivateKeyArgs{
		Algorithm:  pulumi.String("ECDSA"),
		EcdsaCurve: pulumi.String("P256"),
	})
	if err != nil {
		return nil, err
	}
	csr, err := tls.NewCertRequest(ctx, "keycloak-csr", &tls.CertRequestArgs{
		PrivateKeyPem: srvKey.PrivateKeyPem,
		DnsNames: pulumi.StringArray{
			pulumi.String(cfg.Hostname),
			pulumi.String("localhost"),
			pulumi.String("keycloak"),
			pulumi.String("keycloak." + cfg.Namespace + ".svc"),
			pulumi.String("keycloak." + cfg.Namespace + ".svc.cluster.local"),
		},
		IpAddresses: pulumi.StringArray{pulumi.String("127.0.0.1")},
		Subject: &tls.CertRequestSubjectArgs{
			CommonName: pulumi.String(cfg.Hostname),
		},
	})
	if err != nil {
		return nil, err
	}
	srvCert, err := tls.NewLocallySignedCert(ctx, "keycloak-cert", &tls.LocallySignedCertArgs{
		CertRequestPem:      csr.CertRequestPem,
		CaPrivateKeyPem:     caKey.PrivateKeyPem,
		CaCertPem:           caCert.CertPem,
		ValidityPeriodHours: pulumi.Int(24 * 365),
		EarlyRenewalHours:   pulumi.Int(24 * 30),
		AllowedUses: pulumi.StringArray{
			pulumi.String("digital_signature"),
			pulumi.String("key_encipherment"),
			pulumi.String("server_auth"),
		},
	})
	if err != nil {
		return nil, err
	}

	return &Certificates{
		CACertPEM:     caCert.CertPem,
		ServerCertPEM: pulumi.Sprintf("%s%s", srvCert.CertPem, caCert.CertPem),
		ServerKeyPEM:  srvKey.PrivateKeyPem,
	}, nil
}
