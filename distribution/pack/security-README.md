# TLS material

CDS serves HTTPS only and refuses to start without a certificate and a private
key, and it calls the Identity Server over HTTPS too, so this directory holds
both sides:

```
server.crt      the certificate CDS presents, PEM
server.key      its private key, PEM, mode 600
trust_store.pem the Identity Server certificates CDS trusts, PEM
```

`tls.cert_dir` in `repository/conf/deployment.yaml` points here. A relative
path is resolved against the CDS home.

## What this pack ships

All three files are **WSO2 Identity Server 7.3.0's default key pair** — the
`wso2carbon` entry of its `wso2carbon.p12`. A default Identity Server 7.3.0
already trusts that certificate in its `client-truststore.p12`, and CDS trusts
it here, so the two call each other over TLS with no certificate set up on
either side.

That holds only while they reach each other as **`localhost`**: the
certificate is issued for `localhost` and nothing else. It suits a first
installation with both on one host. Containers, separate hosts, or any other
name need the certificates below.

## Replace it for production

**This key pair is a convenience for getting started, not a credential.** Its
private key is in every Identity Server 7.3.0 download, so anyone can present
themselves as this CDS — or as that Identity Server. `cds verify` counts it
as a failed check while it is in use.

Replace it on both sides:

1. Here: `server.crt` and `server.key` with a certificate issued for the host
   in `server_url`, the name the Identity Server and the Console reach CDS on.
2. Here: `trust_store.pem` with the certificate the Identity Server presents,
   or the CA that issued it.
3. On the Identity Server: import the new `server.crt`, or its CA, into
   `client-truststore.p12`, and restart it.

In a container, mount the files rather than changing the image: this directory
for CDS, and the truststore for the Identity Server.
