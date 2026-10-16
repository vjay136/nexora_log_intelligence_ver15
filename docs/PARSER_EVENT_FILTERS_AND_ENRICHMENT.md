# Event filtering and derived fields

The client parses every record by default. To parse only selected native event
names or Webtop methods, edit the relevant input's `selection` in
`/etc/nli/nli.json` and restart `nli-agent`.

Native mail services use `events`; Webtop/Webmail uses a separate `methods`
allowlist. Selectors are literal, case-insensitive, and combined with AND across
the configured dimensions. Use `filtered` to ignore records outside the list,
or `hybrid` to send nonmatching records through normal automatic parser
detection. A missing event or method is not a match when its allowlist is set.

Example for IMAP `ImapDisconnected` and Webtop `mail.message.list`:

```json
{
  "path": "/var/log/nli/input/imap.log",
  "service": "imap",
  "selection": {
    "mode": "filtered",
    "events": ["ImapDisconnected"]
  }
}
```

```json
{
  "path": "/var/log/nli/input/webtop.log",
  "service": "webtop",
  "selection": {
    "mode": "filtered",
    "methods": ["mail.message.list"]
  }
}
```

The checked-in client config points to the GeoLite databases at
`/etc/geoip-db/GeoLite2-Country.mmdb`, `/etc/geoip-db/GeoLite2-ASN.mmdb`, and
`/etc/geoip-db/GeoLite2-City.mmdb`. Missing databases are skipped; an existing
database that is unreadable or malformed prevents the parser service from
starting, so operators see the configuration failure. Private IPs receive
`is_private_ip: true`, and GeoIP country, ASN, and city fields are omitted.
Public IPs only receive the fields present in the corresponding database.

Successful parsed events receive `email_bucket` when a user/email field exists
and `network_bucket` plus `is_private_ip` when an IP is recognized from
`fromhost`, `remoteaddr`, `client_address`, `clientaddr`, or `ip`. The buckets
are intended for grouping in outputs; do not configure raw `user` or IP fields
as Loki labels. Event fields remain available in the JSON payload according to
the selected output contract.
