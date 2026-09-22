package main

import (
	"fmt"

	"github.com/pulumi/pulumi-command/sdk/go/command/local"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Cluster struct {
	Kubeconfig pulumi.StringOutput
}

// newCluster provisions a single-node k3s cluster (Rancher) in Docker via k3d.
//
// Hardening choices:
//   - Kubernetes API bound to 127.0.0.1 only.
//   - Only ONE host port is published (127.0.0.1:<httpsPort> -> 443), no HTTP port.
//   - Traefik is disabled: Keycloak terminates TLS itself, so there is no
//     plaintext hop and one less exposed component.
func newCluster(ctx *pulumi.Context, cfg Config) (*Cluster, error) {
	create := fmt.Sprintf(
		`k3d cluster list %[1]s >/dev/null 2>&1 || k3d cluster create %[1]s `+
			`--image %[2]s --servers 1 --agents 0 `+
			`--api-port 127.0.0.1:%[3]d `+
			`--port "127.0.0.1:%[4]d:443@loadbalancer" `+
			`--k3s-arg "--disable=traefik@server:0" `+
			`--kubeconfig-update-default=false --kubeconfig-switch-context=false `+
			`--wait --timeout 300s`,
		cfg.ClusterName, cfg.K3sImage, cfg.APIPort, cfg.HTTPSPort)

	clusterCmd, err := local.NewCommand(ctx, "k3d-cluster", &local.CommandArgs{
		Create: pulumi.String(create),
		Delete: pulumi.String(fmt.Sprintf("k3d cluster delete %s", cfg.ClusterName)),
	})
	if err != nil {
		return nil, err
	}

	kubeconfigCmd, err := local.NewCommand(ctx, "k3d-kubeconfig", &local.CommandArgs{
		Create:   pulumi.String(fmt.Sprintf("k3d kubeconfig get %s", cfg.ClusterName)),
		Triggers: pulumi.Array{clusterCmd.ID()},
	},
		pulumi.DependsOn([]pulumi.Resource{clusterCmd}),
		pulumi.AdditionalSecretOutputs([]string{"stdout"}),
	)
	if err != nil {
		return nil, err
	}

	return &Cluster{Kubeconfig: pulumi.ToSecret(kubeconfigCmd.Stdout).(pulumi.StringOutput)}, nil
}
