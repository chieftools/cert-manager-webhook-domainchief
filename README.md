# [cert-manager](https://cert-manager.io/) [Domain Chief](https://domain.chief.app?ref=cert-manager-webhook-domainchief) webhook

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cert-manager-webhook-domainchief)](https://artifacthub.io/packages/search?repo=cert-manager-webhook-domainchief)
[![Release](https://img.shields.io/github/v/release/chieftools/cert-manager-webhook-domainchief)](https://github.com/chieftools/cert-manager-webhook-domainchief/releases)
[![CI](https://github.com/chieftools/cert-manager-webhook-domainchief/actions/workflows/ci.yml/badge.svg)](https://github.com/chieftools/cert-manager-webhook-domainchief/actions/workflows/ci.yml)

This webhook enables [DNS01 validation](https://cert-manager.io/docs/configuration/acme/dns01/) with cert-manager when using [Domain Chief](https://domain.chief.app?ref=cert-manager-webhook-domainchief) hosted DNS.

## Installation

cert-manager must be installed first, see the [cert-manager installation docs](https://cert-manager.io/docs/installation/).

Install the webhook into the same namespace as cert-manager:

```bash
helm install cert-manager-webhook-domainchief \
  oci://ghcr.io/chieftools/charts/cert-manager-webhook-domainchief \
  --namespace cert-manager
```

and upgrade it using:

```bash
helm upgrade cert-manager-webhook-domainchief \
  oci://ghcr.io/chieftools/charts/cert-manager-webhook-domainchief \
  --namespace cert-manager
```

## Credentials

You need to supply the webhook with a Domain Chief API key. You can create a new API key [here](https://account.chief.app/team/select/token/create?scopes=domainchief:acme:write&ref=cert-manager-webhook-domainchief), select the team that owns your domains and ensure the token has only the `domainchief:acme:write` scope.

The `domainchief:acme:write` scope only allows publishing and deleting TXT records on `_acme-challenge` names, it cannot read your domains or change any other DNS record. Keep in mind that anyone holding the key can complete DNS01 challenges and obtain certificates for the domains in your team.

Store the API key in a Secret:

```bash
kubectl -n cert-manager create secret generic domainchief-api-key --from-literal=api-key=ctt_...
```

By default the webhook may only read the Secret named `domainchief-api-key` in the `cert-manager` namespace. For a `ClusterIssuer` the Secret must live in cert-manager's [cluster resource namespace](https://cert-manager.io/docs/configuration/#cluster-resource-namespace) (usually `cert-manager`), for an `Issuer` it must live in the same namespace as the `Issuer`. Use the `secretAccess.namespaces` and `secretAccess.secretNames` chart values to change which Secrets the webhook may read.

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
    email: you@example.com
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

And request a certificate, wildcards are supported:

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

The domain must use Domain Chief hosted DNS. Challenge records are always published on the `_acme-challenge` name of the domain, so the `cnameStrategy: Follow` solver option is not supported.

## Verifying signatures

Images and charts are signed with [cosign](https://docs.sigstore.dev/cosign/) using keyless signing from the release workflow. Verify them using:

```bash
cosign verify ghcr.io/chieftools/cert-manager-webhook-domainchief:<version> \
  --certificate-identity-regexp '^https://github\.com/chieftools/cert-manager-webhook-domainchief/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

cosign verify ghcr.io/chieftools/charts/cert-manager-webhook-domainchief:<version> \
  --certificate-identity-regexp '^https://github\.com/chieftools/cert-manager-webhook-domainchief/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Development

Run the unit tests:

```bash
make test
```

Run the [cert-manager conformance suite](https://github.com/cert-manager/cert-manager/tree/master/test/acme) against the real Domain Chief API, this requires a domain using hosted DNS and an API key with the `domainchief:acme:write` scope:

```bash
TEST_ZONE_NAME=example.com. DOMAINCHIEF_API_KEY=ctt_... make test-conformance
```

## Releasing

Push a `vX.Y.Z` tag, this publishes the `ghcr.io/chieftools/cert-manager-webhook-domainchief` image and the `oci://ghcr.io/chieftools/charts/cert-manager-webhook-domainchief` chart and creates a GitHub release.
