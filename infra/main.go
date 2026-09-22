package main

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := loadConfig(ctx)

		// 1. Local Kubernetes cluster (Rancher k3s via k3d).
		cluster, err := newCluster(ctx, cfg)
		if err != nil {
			return err
		}

		// 2. Kubernetes provider bound to the freshly created cluster only.
		k8s, err := kubernetes.NewProvider(ctx, "k3d", &kubernetes.ProviderArgs{
			Kubeconfig: cluster.Kubeconfig,
		})
		if err != nil {
			return err
		}
		opts := []pulumi.ResourceOption{pulumi.Provider(k8s)}

		// 3. TLS material (private CA + server certificate).
		certs, err := newCertificates(ctx, cfg)
		if err != nil {
			return err
		}

		// 4. Namespace, secrets, network policies, Postgres, Keycloak.
		ns, err := newNamespace(ctx, cfg, opts)
		if err != nil {
			return err
		}
		secrets, err := newSecrets(ctx, cfg, ns, certs, opts)
		if err != nil {
			return err
		}
		if err := newNetworkPolicies(ctx, ns, opts); err != nil {
			return err
		}
		pg, err := newPostgres(ctx, cfg, ns, secrets, opts)
		if err != nil {
			return err
		}
		if err := newKeycloak(ctx, cfg, ns, secrets, pg, opts); err != nil {
			return err
		}

		url := fmt.Sprintf("https://%s:%d", cfg.Hostname, cfg.HTTPSPort)
		ctx.Export("keycloakUrl", pulumi.String(url))
		ctx.Export("adminConsoleUrl", pulumi.String(url+"/admin/"))
		ctx.Export("adminUsername", pulumi.String(cfg.AdminUser))
		ctx.Export("adminPassword", secrets.AdminPassword)
		ctx.Export("caCertificate", certs.CACertPEM)
		ctx.Export("kubeconfig", cluster.Kubeconfig)
		return nil
	})
}
