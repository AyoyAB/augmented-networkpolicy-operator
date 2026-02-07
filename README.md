# Augmented NetworkPolicy Operator

A Kubernetes operator that extends NetworkPolicy with DNS-based egress rules. Define egress rules using hostnames instead of IP addresses -- the operator resolves them to IPs and manages standard `networking.k8s.io/v1` NetworkPolicy resources automatically.

## How it works

1. You create a `networking.ayoy.se/v1alpha1` NetworkPolicy with hostname-based egress rules
2. The operator resolves hostnames to IP addresses
3. A standard Kubernetes NetworkPolicy is created with `ipBlock` entries for the resolved IPs
4. DNS is periodically re-resolved (default: every 5 minutes) and the NetworkPolicy is updated if addresses change
5. Deleting the custom resource automatically garbage-collects the standard NetworkPolicy via owner references

## Example

```yaml
apiVersion: networking.ayoy.se/v1alpha1
kind: NetworkPolicy
metadata:
  name: allow-api-egress
spec:
  podSelector:
    matchLabels:
      app: web
  policyTypes:
    - Egress
  egress:
    - ports:
        - protocol: TCP
          port: 443
      to:
        - hostname: api.example.com
  resolutionInterval: 10m
```

This produces a standard NetworkPolicy with resolved IPs:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-api-egress
spec:
  podSelector:
    matchLabels:
      app: web
  policyTypes:
    - Egress
  egress:
    - ports:
        - protocol: TCP
          port: 443
      to:
        - ipBlock:
            cidr: 93.184.216.34/32
```

## CRD reference

| Field | Type | Description |
|---|---|---|
| `spec.podSelector` | `LabelSelector` | Selects pods this policy applies to |
| `spec.policyTypes` | `[]PolicyType` | `Egress` (only egress is supported) |
| `spec.egress[].to[].hostname` | `string` | DNS hostname to resolve |
| `spec.egress[].ports[]` | `NetworkPolicyPort` | Standard port/protocol definitions |
| `spec.resolutionInterval` | `Duration` | DNS re-resolution interval (default `5m`, minimum `30s`) |
| `status.conditions` | `[]Condition` | `Ready` condition with resolution status |
| `status.resolvedAddresses` | `map[string][]string` | Hostname to resolved CIDRs |

## Installation

### Helm

```bash
helm install augmented-networkpolicy-operator \
  oci://ghcr.io/ayoy/augmented-networkpolicy-operator \
  --namespace augmented-networkpolicy-operator-system --create-namespace
```

See [charts/augmented-networkpolicy-operator/README.md](charts/augmented-networkpolicy-operator/README.md) for all available values.

### Kustomize

```bash
kubectl apply -k config/default
```

## Metrics

The operator exposes Prometheus metrics on the metrics endpoint (default `:8443`, HTTPS):

| Metric | Type | Description |
|---|---|---|
| `augmented_networkpolicy_creations_total` | Counter | Standard NetworkPolicies created |
| `augmented_networkpolicy_deletions_total` | Counter | Custom NetworkPolicies detected as deleted |
| `augmented_networkpolicy_dns_changes_total` | Counter | Standard NetworkPolicy updates due to DNS changes |

## Security considerations

### Rate limiting with ResourceQuota

Each augmented NetworkPolicy triggers DNS resolution for every hostname in its egress rules. In multi-tenant clusters, a user could create many resources with many hostnames, causing a burst of DNS queries. Use Kubernetes `ResourceQuota` to limit the number of custom resources per namespace:

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: networkpolicy-quota
spec:
  hard:
    count/networkpolicies.networking.ayoy.se: "10"
```

### Hostname validation

The CRD schema enforces that hostnames must:
- Be between 1 and 253 characters
- Match the pattern `^[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$`

### Resolution interval

The minimum resolution interval is clamped to 30 seconds to prevent excessive DNS lookups. Values below this floor are silently raised.

## Development

### Prerequisites

- Go 1.25+
- Docker
- [Kind](https://kind.sigs.k8s.io/) (for local testing)

### Build and test

```bash
make build          # Build the manager binary
make test           # Run unit tests (envtest)
make lint           # Run golangci-lint
```

### Run locally against a Kind cluster

```bash
make kind-create    # Create a Kind cluster
make kind-load      # Build image and load into Kind
make deploy         # Deploy with kustomize
make test-e2e       # Run Chainsaw e2e tests
make kind-delete    # Tear down the cluster
```

### Helm docs

Helm values documentation is generated with [helm-docs](https://github.com/norwoodj/helm-docs):

```bash
make helm-docs
```

## License

Apache License 2.0 -- see [LICENSE](LICENSE).
