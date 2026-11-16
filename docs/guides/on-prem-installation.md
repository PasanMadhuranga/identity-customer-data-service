# Installing CDS on-premises

CDS runs as its own process beside a WSO2 Identity Server. The two are
separate installations with separate lifecycles, and neither writes into the
other: CDS is extracted and configured on its own, and the Identity Server
carries its own side of the integration — the CDS extension bundles and the
configuration they need — switched on in its own `deployment.toml`.

So there is no step that reaches from one installation into the other, which
is also what lets either one run from an image or a read-only mount.

## Path A — CDS first

Use this when you start from the CDS download page. Extracting the zip *is*
the installation; there is nothing to run to complete it.

```bash
unzip cds-linux-amd64-1.2.0.zip -d /opt
cd /opt/cds-linux-amd64-1.2.0

# Review the licence and required notices.
less LICENSE.txt

./start.sh
```

A pack is extracted onto a machine where nothing else has been installed, so
it defaults to the inbuilt database and needs no database server to start.
Switch `datasource` in `repository/conf/deployment.yaml` to PostgreSQL for a
production deployment — see the [README](../../README.md#-database) for which
to choose.

## Path B — Identity Server first

Use this when the Identity Server is already installed and CDS is being added.
The CDS executable does not exist yet, so acquisition starts from a bootstrap
installer:

```bash
./install_cds.sh --is-home /opt/wso2is
```

It reads the installed Identity Server version, resolves a CDS release that
declares support for it, detects the platform, downloads and verifies the
matching zip, presents the licence, and extracts it. It does not touch the
Identity Server. It ends by printing the next two steps, which are the ones
below.

Both paths end at the same `CDS_HOME`.

## Configuring CDS

`repository/conf/deployment.yaml` is the whole configuration. A pack's
defaults already match a default Identity Server on the same host:

| Setting | Pack default | Change it when |
|---|---|---|
| `auth_server.host`, `auth_server.port` | `localhost`, `9443` | the Identity Server is elsewhere |
| `addr.host`, `addr.port` | `127.0.0.1`, `8900` | CDS has to listen on another address |
| `server_url` | `https://localhost:8900` | the Identity Server reaches CDS by another name |
| `tls.cert_dir` | `repository/resources/security` | the TLS material is mounted elsewhere |
| `datasource` | the inbuilt database | in production — PostgreSQL |
| `application_identifier_type` | `app_id` | the Console identifies applications by client ID; the two have to match |

Credentials are not written into it; see [Secrets](#secrets).

**Set `SERVICE_CREDENTIALS_ENCRYPTION_KEY` before switching CDS on.** When an
organization enables CDS, the Identity Server creates an application for CDS
in that organization and sends CDS its credentials. CDS calls the Identity
Server for that organization with them, and introspects its tokens with them,
and it keeps them only sealed with this key (`idp.service_credentials.encryption_key`).
Without a key it refuses them, and uses the clients configured under
`auth_server` for every organization instead. Generate one with:

```bash
openssl rand -base64 32
```

Keep it safe and keep it the same: credentials sealed with one key cannot be
read with another.

## Switching CDS on in the Identity Server

Add one block to the Identity Server's `repository/conf/deployment.toml` and
restart it:

```toml
[customer_data_service]
enable = true
server_url = "https://localhost:8900"
admin_username = "admin"
admin_password = "<AUTH_SERVER_ADMIN_PASSWORD of CDS>"
```

`server_url` is where the Identity Server reaches CDS. `admin_username` and
`admin_password` are the credentials it presents on the CDS synchronisation
endpoints, so they have to match `auth_server.admin_username` and
`AUTH_SERVER_ADMIN_PASSWORD` on the CDS side.

Everything else the integration needs follows from `enable = true` in an
Identity Server release that carries the CDS integration: the event handlers,
the CDS API resources and their scopes, and the Console's API resource
collection. The Identity Server versions a CDS release supports are listed in
its release metadata.

CDS is then enabled per organization, in the Identity Server — from the
Console, or with `PATCH /api/server/v1/configs/cds`. That is also what runs
the organization's first profile-schema synchronisation and seeds its default
consent categories; until it has happened, the profile, schema and rule
endpoints refuse requests.

## Running CDS

```bash
./start.sh            # or ./cds start
./start.sh --debug    # logging at DEBUG
```

CDS runs in the foreground and logs to standard output. Ctrl+C stops it.
There is no background mode, and so no stop or status command. A start whose
address is already taken is refused up front rather than left to fail inside
the server's startup.

Running it as a service is left to whatever already runs services, each of
which wants exactly this — one process in the foreground, its logs on standard
output, and a signal to end it. With systemd:

```ini
[Unit]
Description=WSO2 Customer Data Service
After=network-online.target

[Service]
User=cds
WorkingDirectory=/opt/cds
ExecStart=/opt/cds/cds start
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

and its logs are then `journalctl -u cds`.

## Certificates

CDS and the Identity Server call each other over HTTPS, so each has to trust
the other's certificate.

A pack carries WSO2 Identity Server 7.3.0's default key pair as its
`server.crt` and `server.key`, and trusts that certificate in
`trust_store.pem`. A default Identity Server 7.3.0 already trusts the same
certificate, so on one host the two talk to each other with no certificate set
up on either side.

The certificate is issued for `localhost` only. Containers, separate hosts, or
any other name need certificates issued for the names in use, on both sides —
see `repository/resources/security/README.md` in the pack.

**Its private key is in every Identity Server 7.3.0 download.** Anyone holding
it can present themselves as that CDS, or as that Identity Server. Replace it
before a deployment carries real traffic; `cds verify` counts the default as a
failed check.

## Verification

```bash
./cds verify
```

It changes nothing, and reads nothing of the Identity Server's but what it
serves over the network, so it answers the same wherever the Identity Server
runs. It checks that:

- CDS is ready at its configured address, with its datastore reachable
- CDS presents a certificate issued for this deployment rather than the
  pack's default, and one valid for the host in `server_url`
- the Identity Server is reachable at `auth_server`, and its certificate is
  trusted by the trust store CDS itself uses

## Secrets

Credentials never go into `deployment.yaml`. They reach the runtime as the
environment variables the `${...}` placeholders in the configuration expand
from — `AUTH_SERVER_ADMIN_PASSWORD`, `SERVICE_CREDENTIALS_ENCRYPTION_KEY`,
`DB_PASSWORD` and the like.

Set them in the environment, or keep them in `repository/conf/secrets.env`,
one `KEY=VALUE` per line, readable by the account that runs CDS and nobody
else. `cds start` reads that file, and a variable already set in the
environment wins over it, so a deployment with a secret manager supplies them
from there and needs no file.

## Layout

```
<CDS_HOME>/
├── cds                             the runtime, and `verify`
├── start.sh                        runs it in the foreground
├── repository/
│   ├── conf/
│   │   ├── deployment.yaml         the configuration
│   │   ├── compatibility.yaml      the Identity Server versions this release supports
│   │   └── secrets.env             optional, written by the operator, mode 600
│   ├── database/                   the inbuilt database, when one is used
│   ├── dbscripts/                  DDL for an external database
│   └── resources/security/         the TLS material
├── updates/
│   └── product.txt                 the update-service identity of this pack
├── version.txt
├── LICENSE.txt
└── README.md
```

## Air-gapped installations

A pack needs no network access to install or run beyond the Identity Server it
calls. The bootstrap installer fetches the release, so point it at an internal
mirror of the releases, laid out as the releases are — `<tag>/<file>`:

```bash
./install_cds.sh --is-home /opt/wso2is --mirror https://mirror.example.com/cds
```

It resolves the release and verifies the checksum exactly as it does against
the public releases; only where the bytes come from changes.
