# augmented-networkpolicy-operator

A Helm chart for the Augmented NetworkPolicy Operator

## Installation

```bash
helm repo add augmented-networkpolicy-operator oci://ghcr.io/ayoy
helm install augmented-networkpolicy-operator augmented-networkpolicy-operator/augmented-networkpolicy-operator \
  --namespace augmented-networkpolicy-operator-system --create-namespace
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for pod scheduling |
| fullnameOverride | string | `""` | Override the full resource name |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.repository | string | `"ghcr.io/ayoy/augmented-networkpolicy-operator"` | Container image repository |
| image.tag | string | `"latest"` | Image tag (defaults to chart appVersion) |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries |
| leaderElection.enabled | bool | `true` | Enable leader election for high availability |
| metrics.port | int | `8080` | Port for the Prometheus metrics endpoint |
| metrics.secure | bool | `false` | Serve metrics over HTTPS (set to true for production) |
| nameOverride | string | `""` | Override the chart name |
| nodeSelector | object | `{}` | Node selector for pod scheduling |
| replicaCount | int | `1` | Number of controller replicas |
| resources | object | `{"limits":{"cpu":"500m","memory":"128Mi"},"requests":{"cpu":"10m","memory":"64Mi"}}` | CPU/memory resource requests and limits |
| serviceAccount.annotations | object | `{}` | Annotations to add to the ServiceAccount |
| serviceAccount.create | bool | `true` | Create a ServiceAccount for the controller |
| serviceAccount.name | string | `""` | Override the ServiceAccount name (defaults to fullname) |
| tolerations | list | `[]` | Tolerations for pod scheduling |

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| ayoy |  |  |
