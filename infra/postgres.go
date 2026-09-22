package main

import (
	appsv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// newPostgres deploys a single-instance PostgreSQL as Keycloak's persistent
// store (the embedded dev H2 database is not suitable for real use).
// ClusterIP only: it is never reachable from outside the cluster.
func newPostgres(ctx *pulumi.Context, cfg Config, ns *corev1.Namespace, s *Secrets, opts []pulumi.ResourceOption) (*appsv1.StatefulSet, error) {
	const uid = 70 // "postgres" user in the alpine image
	labels := appLabels("postgres")

	sa, err := newServiceAccount(ctx, ns, "postgres", opts)
	if err != nil {
		return nil, err
	}

	svc, err := corev1.NewService(ctx, "postgres-svc", &corev1.ServiceArgs{
		Metadata: meta(ns, "postgres", labels),
		Spec: &corev1.ServiceSpecArgs{
			Type:     pulumi.String("ClusterIP"),
			Selector: selector("postgres"),
			Ports: corev1.ServicePortArray{&corev1.ServicePortArgs{
				Name: pulumi.String("postgres"), Port: pulumi.Int(5432), TargetPort: pulumi.String("postgres"),
			}},
		},
	}, opts...)
	if err != nil {
		return nil, err
	}

	secretEnv := func(name, key string) *corev1.EnvVarArgs {
		return &corev1.EnvVarArgs{
			Name: pulumi.String(name),
			ValueFrom: &corev1.EnvVarSourceArgs{SecretKeyRef: &corev1.SecretKeySelectorArgs{
				Name: pulumi.String(dbSecretName), Key: pulumi.String(key),
			}},
		}
	}
	isReady := &corev1.ExecActionArgs{Command: pulumi.StringArray{
		pulumi.String("pg_isready"), pulumi.String("-U"), pulumi.String(dbUser), pulumi.String("-d"), pulumi.String(dbName),
	}}

	deps := append([]pulumi.Resource{svc, sa}, s.Resources...)
	return appsv1.NewStatefulSet(ctx, "postgres", &appsv1.StatefulSetArgs{
		Metadata: meta(ns, "postgres", labels),
		Spec: &appsv1.StatefulSetSpecArgs{
			ServiceName: pulumi.String("postgres"),
			Replicas:    pulumi.Int(1),
			Selector:    &metav1.LabelSelectorArgs{MatchLabels: selector("postgres")},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{Labels: labels},
				Spec: &corev1.PodSpecArgs{
					ServiceAccountName:           pulumi.String("postgres"),
					AutomountServiceAccountToken: pulumi.Bool(false),
					EnableServiceLinks:           pulumi.Bool(false),
					SecurityContext: &corev1.PodSecurityContextArgs{
						RunAsNonRoot:   pulumi.Bool(true),
						RunAsUser:      pulumi.Int(uid),
						RunAsGroup:     pulumi.Int(uid),
						FsGroup:        pulumi.Int(uid),
						SeccompProfile: &corev1.SeccompProfileArgs{Type: pulumi.String("RuntimeDefault")},
					},
					Containers: corev1.ContainerArray{&corev1.ContainerArgs{
						Name:  pulumi.String("postgres"),
						Image: pulumi.String(cfg.PostgresImage),
						Ports: corev1.ContainerPortArray{&corev1.ContainerPortArgs{
							Name: pulumi.String("postgres"), ContainerPort: pulumi.Int(5432),
						}},
						Env: corev1.EnvVarArray{
							&corev1.EnvVarArgs{Name: pulumi.String("POSTGRES_DB"), Value: pulumi.String(dbName)},
							secretEnv("POSTGRES_USER", "username"),
							secretEnv("POSTGRES_PASSWORD", "password"),
							&corev1.EnvVarArgs{Name: pulumi.String("PGDATA"), Value: pulumi.String("/var/lib/postgresql/data/pgdata")},
						},
						SecurityContext: restrictedContainerSC(uid, true),
						VolumeMounts: corev1.VolumeMountArray{
							&corev1.VolumeMountArgs{Name: pulumi.String("data"), MountPath: pulumi.String("/var/lib/postgresql/data")},
							&corev1.VolumeMountArgs{Name: pulumi.String("run"), MountPath: pulumi.String("/var/run/postgresql")},
							&corev1.VolumeMountArgs{Name: pulumi.String("tmp"), MountPath: pulumi.String("/tmp")},
						},
						ReadinessProbe: &corev1.ProbeArgs{Exec: isReady, PeriodSeconds: pulumi.Int(5), InitialDelaySeconds: pulumi.Int(5)},
						LivenessProbe:  &corev1.ProbeArgs{Exec: isReady, PeriodSeconds: pulumi.Int(10), InitialDelaySeconds: pulumi.Int(30)},
						Resources: &corev1.ResourceRequirementsArgs{
							Requests: pulumi.StringMap{"cpu": pulumi.String("100m"), "memory": pulumi.String("256Mi")},
							Limits:   pulumi.StringMap{"cpu": pulumi.String("1"), "memory": pulumi.String("512Mi")},
						},
					}},
					Volumes: corev1.VolumeArray{
						&corev1.VolumeArgs{Name: pulumi.String("run"), EmptyDir: &corev1.EmptyDirVolumeSourceArgs{}},
						&corev1.VolumeArgs{Name: pulumi.String("tmp"), EmptyDir: &corev1.EmptyDirVolumeSourceArgs{}},
					},
				},
			},
			VolumeClaimTemplates: corev1.PersistentVolumeClaimTypeArray{&corev1.PersistentVolumeClaimTypeArgs{
				Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("data")},
				Spec: &corev1.PersistentVolumeClaimSpecArgs{
					AccessModes: pulumi.StringArray{pulumi.String("ReadWriteOnce")},
					Resources: &corev1.VolumeResourceRequirementsArgs{
						Requests: pulumi.StringMap{"storage": pulumi.String("2Gi")},
					},
				},
			}},
		},
	}, append(opts, pulumi.DependsOn(deps))...)
}
