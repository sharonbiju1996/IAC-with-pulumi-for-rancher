package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Config holds every tunable of the stack. All values have safe defaults so
// `pulumi up` works with zero configuration.
type Config struct {
	ClusterName   string
	K3sImage      string
	APIPort       int
	HTTPSPort     int
	Hostname      string
	Namespace     string
	KeycloakImage string
	PostgresImage string
	AdminUser     string
	// AdminPassword is optional; when unset a random one is generated.
	AdminPassword    pulumi.StringOutput
	AdminPasswordSet bool
}

func loadConfig(ctx *pulumi.Context) Config {
	c := config.New(ctx, "")
	get := func(key, def string) string {
		if v := c.Get(key); v != "" {
			return v
		}
		return def
	}
	getInt := func(key string, def int) int {
		if v := c.GetInt(key); v != 0 {
			return v
		}
		return def
	}

	cfg := Config{
		ClusterName:   get("clusterName", "keycloak"),
		K3sImage:      get("k3sImage", "rancher/k3s:v1.31.5-k3s1"),
		APIPort:       getInt("apiPort", 6550),
		HTTPSPort:     getInt("httpsPort", 8443),
		Hostname:      get("hostname", "keycloak.localtest.me"),
		Namespace:     get("namespace", "keycloak"),
		KeycloakImage: get("keycloakImage", "quay.io/keycloak/keycloak:26.3"),
		PostgresImage: get("postgresImage", "postgres:16-alpine"),
		AdminUser:     get("adminUsername", "admin"),
	}
	if pw, err := c.TrySecret("adminPassword"); err == nil {
		cfg.AdminPassword = pw
		cfg.AdminPasswordSet = true
	}
	return cfg
}
