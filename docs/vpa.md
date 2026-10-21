# Install Vertical Pod Autoscaler from WSL

Run these commands in WSL Bash with Helm 3 and `kubectl` installed. They target
the production cluster explicitly; confirm its nodes before proceeding.

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get nodes
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" top nodes
```

The second command checks Metrics Server, which VPA needs for recommendations.
Resolve any metrics errors before installing.

## Install VPA once per cluster

This uses [Fairwinds VPA chart](https://github.com/FairwindsOps/charts/tree/master/stable/vpa)
5.1.0 with VPA 1.7.1, compatible with the repository's Kubernetes 1.37 version.
It installs the CRDs, permissions, recommender, updater, admission controller,
and webhook certificates. It uses the existing Metrics Server.

```bash
helm --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" upgrade --install vpa \
  https://charts.fairwinds.com/stable/vpa-5.1.0.tgz \
  --namespace kube-system \
  --set recommender.enabled=true,updater.enabled=true,admissionController.enabled=true,metrics-server.enabled=false \
  --set-string 'updater.extraArgs.feature-gates=InPlace=true' \
  --set-string 'admissionController.extraArgs.feature-gates=InPlace=true' \
  --wait --timeout 5m
```

The feature gates enable the alpha `InPlace` mode used by PostgreSQL, without
eviction fallback. Only workloads with a VPA resource are autoscaled.

Check that the CRD is established and the three controller Deployments are ready:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" wait \
  --for=condition=Established crd/verticalpodautoscalers.autoscaling.k8s.io --timeout=60s
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n kube-system \
  get deployments -l app.kubernetes.io/instance=vpa
```

## Reapply PostgreSQL

From the repository root, rerun the deployment command:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -k applications/postgres
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  get vpa psql-server
```

If the earlier apply created the PVC, Services, and StatefulSet but failed on VPA,
this adds the missing VPA and retains the existing resources. Recommendations
take time to appear. Do not delete the PVC to retry installation.
