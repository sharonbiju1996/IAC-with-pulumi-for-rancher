package main

import (
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	dbSecretName    = "keycloak-db"
	adminSecretName = "keycloak-admin"
	tlsSecretName   = "keycloak-tls"
	dbName          = "keycloak"
	dbUser          = "keycloak"
)

func appLabels(app string) pulumi.StringMap {
	return pulumi.StringMap{
		"app.kubernetes.io/name":       pulumi.String(app),
		"app.kubernetes.io/part-of":    pulumi.String("keycloak-stack"),
		"app.kubernetes.io/managed-by": pulumi.String("pulumi"),
	}
}

func selector(app string) pulumi.StringMap {
	return pulumi.StringMap{"app.kubernetes.io/name": pulumi.String(app)}
}

func meta(ns *corev1.Namespace, name string, labels pulumi.StringMap) *metav1.ObjectMetaArgs {
	return &metav1.ObjectMetaArgs{
		Name:      pulumi.String(name),
		Namespace: ns.Metadata.Name(),
		Labels:    labels,
	}
}

// restrictedContainerSC satisfies the Pod Security "restricted" profile.
func restrictedContainerSC(uid int, readOnlyRoot bool) *corev1.SecurityContextArgs {
	return &corev1.SecurityContextArgs{
		RunAsNonRoot:             pulumi.Bool(true),
		RunAsUser:                pulumi.Int(uid),
		AllowPrivilegeEscalation: pulumi.Bool(false),
		ReadOnlyRootFilesystem:   pulumi.Bool(readOnlyRoot),
		Capabilities:             &corev1.CapabilitiesArgs{Drop: pulumi.StringArray{pulumi.String("ALL")}},
		SeccompProfile:           &corev1.SeccompProfileArgs{Type: pulumi.String("RuntimeDefault")},
	}
}

// newNamespace creates the namespace with the Pod Security "restricted" profile enforced.
func newNamespace(ctx *pulumi.Context, cfg Config, opts []pulumi.ResourceOption) (*corev1.Namespace, error) {
	return corev1.NewNamespace(ctx, "keycloak-ns", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(cfg.Namespace),
			Labels: pulumi.StringMap{
				"pod-security.kubernetes.io/enforce":         pulumi.String("restricted"),
				"pod-security.kubernetes.io/enforce-version": pulumi.String("latest"),
				"pod-security.kubernetes.io/audit":           pulumi.String("restricted"),
				"pod-security.kubernetes.io/warn":            pulumi.String("restricted"),
			},
		},
	}, opts...)
}

type Secrets struct {
	AdminPassword pulumi.StringOutput
	Resources     []pulumi.Resource
}

// newSecrets generates credentials (never hard-coded) and stores them as
// Kubernetes Secrets. Pulumi keeps them encrypted in its state.
func newSecrets(ctx *pulumi.Context, cfg Config, ns *corev1.Namespace, certs *Certificates, opts []pulumi.ResourceOption) (*Secrets, error) {
	dbPw, err := random.NewRandomPassword(ctx, "db-password", &random.RandomPasswordArgs{
		Length:  pulumi.Int(32),
		Special: pulumi.Bool(false),
	})
	if err != nil {
		return nil, err
	}

	adminPw := cfg.AdminPassword
	if !cfg.AdminPasswordSet {
		gen, err := random.NewRandomPassword(ctx, "admin-password", &random.RandomPasswordArgs{
			Length:          pulumi.Int(24),
			Special:         pulumi.Bool(true),
			OverrideSpecial: pulumi.String("!#%*-_=+"),
			MinUpper:        pulumi.Int(2),
			MinLower:        pulumi.Int(2),
			MinNumeric:      pulumi.Int(2),
			MinSpecial:      pulumi.Int(2),
		})
		if err != nil {
			return nil, err
		}
		adminPw = gen.Result
	}

	db, err := corev1.NewSecret(ctx, "db-secret", &corev1.SecretArgs{
		Metadata: meta(ns, dbSecretName, appLabels("postgres")),
		Type:     pulumi.String("Opaque"),
		StringData: pulumi.StringMap{
			"username": pulumi.String(dbUser),
			"password": dbPw.Result,
		},
	}, opts...)
	if err != nil {
		return nil, err
	}

	admin, err := corev1.NewSecret(ctx, "admin-secret", &corev1.SecretArgs{
		Metadata: meta(ns, adminSecretName, appLabels("keycloak")),
		Type:     pulumi.String("Opaque"),
		StringData: pulumi.StringMap{
			"username": pulumi.String(cfg.AdminUser),
			"password": adminPw,
		},
	}, opts...)
	if err != nil {
		return nil, err
	}

	tlsSecret, err := corev1.NewSecret(ctx, "tls-secret", &corev1.SecretArgs{
		Metadata: meta(ns, tlsSecretName, appLabels("keycloak")),
		Type:     pulumi.String("kubernetes.io/tls"),
		StringData: pulumi.StringMap{
			"tls.crt": certs.ServerCertPEM,
			"tls.key": certs.ServerKeyPEM,
		},
	}, opts...)
	if err != nil {
		return nil, err
	}

	return &Secrets{
		AdminPassword: pulumi.ToSecret(adminPw).(pulumi.StringOutput),
		Resources:     []pulumi.Resource{db, admin, tlsSecret},
	}, nil
}

// newServiceAccount creates a dedicated SA that does not mount an API token:
// neither Keycloak nor Postgres needs to talk to the Kubernetes API.
func newServiceAccount(ctx *pulumi.Context, ns *corev1.Namespace, name string, opts []pulumi.ResourceOption) (*corev1.ServiceAccount, error) {
	return corev1.NewServiceAccount(ctx, name+"-sa", &corev1.ServiceAccountArgs{
		Metadata:                     meta(ns, name, appLabels(name)),
		AutomountServiceAccountToken: pulumi.Bool(false),
	}, opts...)
}
