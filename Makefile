.PHONY: up down purge credentials status preview
up:           ## Provision cluster + deploy Keycloak
	./scripts/setup.sh
down:         ## Destroy everything (keeps local state)
	./scripts/teardown.sh
purge:        ## Destroy everything and delete local Pulumi state
	PURGE=true ./scripts/teardown.sh
credentials:  ## Print Keycloak admin URL and credentials
	./scripts/credentials.sh
status:       ## Show pods, services, network policies
	kubectl --kubeconfig kubeconfig -n keycloak get pods,svc,pvc,networkpolicy
