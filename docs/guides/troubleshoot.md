# Troubleshoot

Start with the symptom you have. Most of these end at a config
value, so keep [Configuration](../reference/configuration.md) open
in another tab.

- No tag, identity, DNS, or image facts? Check that you passed a
  redaction key file. Without one, those kinds aren't emitted.
- Live run refuses to start? `live` defaults to `false`, and the
  zero-value live factory fails closed. Set the opt-in and make
  sure the workload identity behind `credential_ref` is present.
- Fixture run rejects your target? Fixture pages only serve the
  `resource_graph` and `resource_changes` lanes, and one page list
  carries exactly one lane. Mixed lanes get rejected.
- Result fails on a bound? The run passed a claim limit and failed
  rather than returning late or oversized evidence. Shard the
  scope, then re-check the limits in
  [Configuration](../reference/configuration.md).

## Still stuck?

Re-read the failure flow in
[Diagrams](../diagrams/README.md#3-when-something-goes-wrong) and
check which branch your run took. If the result looks complete but
wrong, compare it against [Facts](../reference/facts.md) before
suspecting the source.
