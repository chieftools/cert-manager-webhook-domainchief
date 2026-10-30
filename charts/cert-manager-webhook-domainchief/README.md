# cert-manager webhook for Domain Chief

This chart installs a [cert-manager](https://cert-manager.io/) webhook that solves [DNS01 challenges](https://cert-manager.io/docs/configuration/acme/dns01/) using [Domain Chief](https://domain.chief.app?ref=cert-manager-webhook-domainchief) hosted DNS, so you can issue certificates (including wildcards) for domains that use Domain Chief hosted DNS.

## Prerequisites

- cert-manager installed in the cluster, see the [cert-manager installation docs](https://cert-manager.io/docs/installation/)
- A domain using Domain Chief hosted DNS

## Installation

Install the webhook into the same namespace as cert-manager:

```bash
helm install cert-manager-webhook-domainchief \
  oci://ghcr.io/chieftools/charts/cert-manager-webhook-domainchief \
  --namespace cert-manager
```

## Credentials

Create a Domain Chief API key [here](https://account.chief.app/team/select/token/create?scopes=domainchief:acme:write&ref=cert-manager-webhook-domainchief), select the team that owns your domains and ensure the token has only the `domainchief:acme:write` scope.

The `domainchief:acme:write` scope only allows publishing and deleting TXT records on `_acme-challenge` names, it cannot read your domains or change any other DNS record.

Store the API key in a Secret:

```bash
kubectl -n cert-manager create secret generic domainchief-api-key --from-literal=api-key=ctt_...
```

By default the webhook may only read the Secret named `domainchief-api-key` in the `cert-manager` namespace. For a `ClusterIssuer` the Secret must live in cert-manager's [cluster resource namespace](https://cert-manager.io/docs/configuration/#cluster-resource-namespace) (usually `cert-manager`), for an `Issuer` it must live in the same namespace as the `Issuer`. Use the `secretAccess.namespaces` and `secretAccess.secretNames` values to change which Secrets the webhook may read.

## Usage

Create a `ClusterIssuer` (or `Issuer`) that uses the webhook as DNS01 solver:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-account-key
    solvers:
      - dns01:
          webhook:
            groupName: acme.domain.chief.app
            solverName: domainchief
            config:
              apiKeySecretRef:
                name: domainchief-api-key
                key: api-key
```

And request a certificate:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com
  namespace: default
spec:
  secretName: example-com-tls
  issuerRef:
    name: letsencrypt
    kind: ClusterIssuer
  dnsNames:
    - example.com
    - '*.example.com'
```

Challenge records are always published on the `_acme-challenge` name of the domain, so the `cnameStrategy: Follow` solver option is not supported.

## Values

| Key | Default | Description |
|---|---|---|
| `groupName` | `acme.domain.chief.app` | API group the webhook registers under, referenced as `groupName` in the issuer. Only change this when running multiple installations in one cluster. |
| `certManager.namespace` | `cert-manager` | Namespace cert-manager is installed in. |
| `certManager.serviceAccountName` | `cert-manager` | Service account cert-manager runs as. |
| `secretAccess.namespaces` | `[]` | Namespaces in which the webhook may read the API key Secret. Defaults to `certManager.namespace` when empty. |
| `secretAccess.secretNames` | `[domainchief-api-key]` | Names of the Secrets the webhook may read. Leave empty to allow every Secret in the namespaces above. |
| `image.repository` | `ghcr.io/chieftools/cert-manager-webhook-domainchief` | Image repository. |
| `image.tag` | `""` | Image tag, defaults to the chart `appVersion`. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `imagePullSecrets` | `[]` | Image pull secrets. |
| `nameOverride` | `""` | Override the chart name. |
| `fullnameOverride` | `""` | Override the full resource name. |
| `replicaCount` | `1` | Number of webhook replicas. |
| `service.type` | `ClusterIP` | Service type. |
| `service.port` | `443` | Service port. |
| `podAnnotations` | `{}` | Annotations added to the webhook pod. |
| `podSecurityContext` | non-root, `RuntimeDefault` seccomp | Pod security context. |
| `securityContext` | read-only root filesystem, no privilege escalation, all capabilities dropped | Container security context. |
| `resources` | `{}` | Container resource requests and limits. |
| `nodeSelector` | `{}` | Node selector. |
| `tolerations` | `[]` | Tolerations. |
| `affinity` | `{}` | Affinity rules. |

## Uninstallation

```bash
helm uninstall cert-manager-webhook-domainchief --namespace cert-manager
```

## Verifying signatures

The chart and image are signed with [cosign](https://docs.sigstore.dev/cosign/) using keyless signing from the release workflow. Verify them using:

```bash
cosign verify ghcr.io/chieftools/charts/cert-manager-webhook-domainchief:<version> \
  --certificate-identity-regexp '^https://github\.com/chieftools/cert-manager-webhook-domainchief/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

cosign verify ghcr.io/chieftools/cert-manager-webhook-domainchief:<version> \
  --certificate-identity-regexp '^https://github\.com/chieftools/cert-manager-webhook-domainchief/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Source and support

The source code is available on [GitHub](https://github.com/chieftools/cert-manager-webhook-domainchief), please report issues there. Security vulnerabilities should be reported privately as described in the [security policy](https://github.com/chieftools/cert-manager-webhook-domainchief/security/policy).
