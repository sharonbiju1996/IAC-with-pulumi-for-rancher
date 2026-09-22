package main

import (
	"fmt"

	appsv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// newKeycloak deploys Keycloak in production mode (`start`):
//   - HTTPS only (HTTP listener disabled), TLS terminated by Keycloak itself
//   - strict hostname, Postgres backend, health/metrics on the internal
//     management port 9000 (not published by the Service)
//   - bootstrap "admin" account taken from a Kubernetes Secret
func newKeycloak(ctx *pulumi.Context, cfg Config, ns *corev1.Namespace, s *Secrets, pg *appsv1.StatefulSet, opts []pulumi.ResourceOption) error {
	const uid = 1000 // "keycloak" user in the official image
	labels := appLabels("keycloak")

	sa, err := newServiceAccount(ctx, ns, "keycloak", opts)
	if err != nil {
		return err
	}

	env := func(k, v string) *corev1.EnvVarArgs {
		return &corev1.EnvVarArgs{Name: pulumi.String(k), Value: pulumi.String(v)}
	}
	secretEnv := func(k, secret, key string) *corev1.EnvVarArgs {
		return &corev1.EnvVarArgs{
			Name: pulumi.String(k),
			ValueFrom: &corev1.EnvVarSourceArgs{SecretKeyRef: &corev1.SecretKeySelectorArgs{
				Name: pulumi.String(secret), Key: pulumi.String(key),
			}},
		}
	}
	probe := func(path string, period, failures int) *corev1.ProbeArgs {
		return &corev1.ProbeArgs{
			HttpGet: &corev1.HTTPGetActionArgs{
				Path: pulumi.String(path), Port: pulumi.String("management"), Scheme: pulumi.String("HTTPS"),
			},
			PeriodSeconds:    pulumi.Int(period),
			FailureThreshold: pulumi.Int(failures),
			TimeoutSeconds:   pulumi.Int(5),
		}
	}

	deps := append([]pulumi.Resource{pg, sa}, s.Resources...)
	deploy, err := appsv1.NewDeployment(ctx, "keycloak", &appsv1.DeploymentArgs{
		Metadata: meta(ns, "keycloak", labels),
		Spec: &appsv1.DeploymentSpecArgs{
			Replicas: pulumi.Int(1),
			// Recreate avoids two Keycloak versions running DB migrations concurrently.
			Strategy: &appsv1.DeploymentStrategyArgs{Type: pulumi.String("Recreate")},
			Selector: &metav1.LabelSelectorArgs{MatchLabels: selector("keycloak")},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{Labels: labels},
				Spec: &corev1.PodSpecArgs{
					ServiceAccountName:           pulumi.String("keycloak"),
					AutomountServiceAccountToken: pulumi.Bool(false),
					EnableServiceLinks:           pulumi.Bool(false),
					SecurityContext: &corev1.PodSecurityContextArgs{
						RunAsNonRoot:   pulumi.Bool(true),
						RunAsUser:      pulumi.Int(uid),
						FsGroup:        pulumi.Int(uid),
						SeccompProfile: &corev1.SeccompProfileArgs{Type: pulumi.String("RuntimeDefault")},
					},
					Containers: corev1.ContainerArray{&corev1.ContainerArgs{
						Name:  pulumi.String("keycloak"),
						Image: pulumi.String(cfg.KeycloakImage),
						Args:  pulumi.StringArray{pulumi.String("start")},
						Ports: corev1.ContainerPortArray{
							&corev1.ContainerPortArgs{Name: pulumi.String("https"), ContainerPort: pulumi.Int(8443)},
							&corev1.ContainerPortArgs{Name: pulumi.String("management"), ContainerPort: pulumi.Int(9000)},
						},
						Env: corev1.EnvVarArray{
							// Admin account
							secretEnv("KC_BOOTSTRAP_ADMIN_USERNAME", adminSecretName, "username"),
							secretEnv("KC_BOOTSTRAP_ADMIN_PASSWORD", adminSecretName, "password"),
							// Database
							env("KC_DB", "postgres"),
							env("KC_DB_URL_HOST", "postgres"),
							env("KC_DB_URL_DATABASE", dbName),
							secretEnv("KC_DB_USERNAME", dbSecretName, "username"),
							secretEnv("KC_DB_PASSWORD", dbSecretName, "password"),
							// Hostname / TLS
							env("KC_HOSTNAME", fmt.Sprintf("https://%s:%d", cfg.Hostname, cfg.HTTPSPort)),
							env("KC_HTTP_ENABLED", "false"),
							env("KC_HTTPS_PORT", "8443"),
							env("KC_HTTPS_PROTOCOLS", "TLSv1.3,TLSv1.2"),
							env("KC_HTTPS_CERTIFICATE_FILE", "/opt/keycloak/conf/tls/tls.crt"),
							env("KC_HTTPS_CERTIFICATE_KEY_FILE", "/opt/keycloak/conf/tls/tls.key"),
							// Observability / runtime
							env("KC_HEALTH_ENABLED", "true"),
							env("KC_METRICS_ENABLED", "true"),
							env("KC_CACHE", "local"),
							env("KC_LOG_LEVEL", "info"),
							env("JAVA_OPTS_KC_HEAP", "-XX:MaxRAMPercentage=70 -XX:InitialRAMPercentage=50"),
						},
						// Root FS stays writable: `start` runs a Quarkus re-augmentation that
						// writes into /opt/keycloak. Everything else follows "restricted".
						SecurityContext: restrictedContainerSC(uid, false),
						VolumeMounts: corev1.VolumeMountArray{
							&corev1.VolumeMountArgs{Name: pulumi.String("tls"), MountPath: pulumi.String("/opt/keycloak/conf/tls"), ReadOnly: pulumi.Bool(true)},
							&corev1.VolumeMountArgs{Name: pulumi.String("tmp"), MountPath: pulumi.String("/tmp")},
						},
						StartupProbe:   probe("/health/started", 5, 60),
						ReadinessProbe: probe("/health/ready", 10, 3),
						LivenessProbe:  probe("/health/live", 15, 3),
						Resources: &corev1.ResourceRequirementsArgs{
							Requests: pulumi.StringMap{"cpu": pulumi.String("500m"), "memory": pulumi.String("1Gi")},
							Limits:   pulumi.StringMap{"cpu": pulumi.String("2"), "memory": pulumi.String("2Gi")},
						},
					}},
					Volumes: corev1.VolumeArray{
						&corev1.VolumeArgs{Name: pulumi.String("tls"), Secret: &corev1.SecretVolumeSourceArgs{
							SecretName: pulumi.String(tlsSecretName), DefaultMode: pulumi.Int(0440),
						}},
						&corev1.VolumeArgs{Name: pulumi.String("tmp"), EmptyDir: &corev1.EmptyDirVolumeSourceArgs{}},
					},
				},
			},
		},
	}, append(opts, pulumi.DependsOn(deps))...)
	if err != nil {
		return err
	}

	// The only externally reachable endpoint: 443 -> Keycloak HTTPS.
	// k3s ServiceLB exposes it on the node; k3d maps it to 127.0.0.1:<httpsPort>.
	_, err = corev1.NewService(ctx, "keycloak-svc", &corev1.ServiceArgs{
		Metadata: meta(ns, "keycloak", labels),
		Spec: &corev1.ServiceSpecArgs{
			Type:     pulumi.String("LoadBalancer"),
			Selector: selector("keycloak"),
			Ports: corev1.ServicePortArray{&corev1.ServicePortArgs{
				Name: pulumi.String("https"), Port: pulumi.Int(443), TargetPort: pulumi.String("https"), Protocol: pulumi.String("TCP"),
			}},
		},
	}, append(opts, pulumi.DependsOn([]pulumi.Resource{deploy}))...)
	return err
}
