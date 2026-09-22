package main

import (
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// newNetworkPolicies implements zero-trust networking inside the namespace.
// k3s enforces NetworkPolicy out of the box (embedded kube-router controller).
//
//	default: deny all ingress + egress
//	all pods      -> CoreDNS (53/udp,tcp)
//	anyone        -> keycloak :8443 (HTTPS)  and :9000 (health, kubelet probes; not published)
//	keycloak      -> postgres :5432
//	everything else is dropped (incl. Keycloak internet egress, Postgres egress)
func newNetworkPolicies(ctx *pulumi.Context, ns *corev1.Namespace, opts []pulumi.ResourceOption) error {
	tcp := pulumi.String("TCP")
	udp := pulumi.String("UDP")

	policies := map[string]*networkingv1.NetworkPolicySpecArgs{
		"default-deny-all": {
			PodSelector: &metav1.LabelSelectorArgs{},
			PolicyTypes: pulumi.StringArray{pulumi.String("Ingress"), pulumi.String("Egress")},
		},
		"allow-dns-egress": {
			PodSelector: &metav1.LabelSelectorArgs{},
			PolicyTypes: pulumi.StringArray{pulumi.String("Egress")},
			Egress: networkingv1.NetworkPolicyEgressRuleArray{&networkingv1.NetworkPolicyEgressRuleArgs{
				To: networkingv1.NetworkPolicyPeerArray{&networkingv1.NetworkPolicyPeerArgs{
					NamespaceSelector: &metav1.LabelSelectorArgs{MatchLabels: pulumi.StringMap{
						"kubernetes.io/metadata.name": pulumi.String("kube-system"),
					}},
					PodSelector: &metav1.LabelSelectorArgs{MatchLabels: pulumi.StringMap{
						"k8s-app": pulumi.String("kube-dns"),
					}},
				}},
				Ports: networkingv1.NetworkPolicyPortArray{
					&networkingv1.NetworkPolicyPortArgs{Port: pulumi.Int(53), Protocol: udp},
					&networkingv1.NetworkPolicyPortArgs{Port: pulumi.Int(53), Protocol: tcp},
				},
			}},
		},
		"keycloak": {
			PodSelector: &metav1.LabelSelectorArgs{MatchLabels: selector("keycloak")},
			PolicyTypes: pulumi.StringArray{pulumi.String("Ingress"), pulumi.String("Egress")},
			Ingress: networkingv1.NetworkPolicyIngressRuleArray{&networkingv1.NetworkPolicyIngressRuleArgs{
				Ports: networkingv1.NetworkPolicyPortArray{
					&networkingv1.NetworkPolicyPortArgs{Port: pulumi.Int(8443), Protocol: tcp},
					&networkingv1.NetworkPolicyPortArgs{Port: pulumi.Int(9000), Protocol: tcp},
				},
			}},
			Egress: networkingv1.NetworkPolicyEgressRuleArray{&networkingv1.NetworkPolicyEgressRuleArgs{
				To: networkingv1.NetworkPolicyPeerArray{&networkingv1.NetworkPolicyPeerArgs{
					PodSelector: &metav1.LabelSelectorArgs{MatchLabels: selector("postgres")},
				}},
				Ports: networkingv1.NetworkPolicyPortArray{
					&networkingv1.NetworkPolicyPortArgs{Port: pulumi.Int(5432), Protocol: tcp},
				},
			}},
		},
		"postgres": {
			PodSelector: &metav1.LabelSelectorArgs{MatchLabels: selector("postgres")},
			PolicyTypes: pulumi.StringArray{pulumi.String("Ingress"), pulumi.String("Egress")},
			Ingress: networkingv1.NetworkPolicyIngressRuleArray{&networkingv1.NetworkPolicyIngressRuleArgs{
				From: networkingv1.NetworkPolicyPeerArray{&networkingv1.NetworkPolicyPeerArgs{
					PodSelector: &metav1.LabelSelectorArgs{MatchLabels: selector("keycloak")},
				}},
				Ports: networkingv1.NetworkPolicyPortArray{
					&networkingv1.NetworkPolicyPortArgs{Port: pulumi.Int(5432), Protocol: tcp},
				},
			}},
		},
	}

	for name, spec := range policies {
		if _, err := networkingv1.NewNetworkPolicy(ctx, "netpol-"+name, &networkingv1.NetworkPolicyArgs{
			Metadata: meta(ns, name, appLabels("keycloak-stack")),
			Spec:     spec,
		}, opts...); err != nil {
			return err
		}
	}
	return nil
}
