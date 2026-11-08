# Facts

You'll get facts about what's in the scope, how resources link to
each other, and what reportedly changed. That's enough to answer
which Azure resources Eshu has seen and how they connect.

The table below mirrors `spec.emittedFacts` in
[manifest.yaml](../../manifest.yaml); update this table in the same
change as the manifest. That file declares 8 fact kinds, all at
schema version `1.0.0` with `reported` confidence (the source
reported it; Eshu didn't measure it directly).

| Fact kind | What you get |
|---|---|
| `azure_cloud_resource` | One Azure resource seen in the scope |
| `azure_cloud_relationship` | One link between two Azure resources |
| `azure_tag_observation` | Tags seen on one Azure resource |
| `azure_identity_observation` | A managed identity or role assignment seen on a resource |
| `azure_dns_record` | One DNS record seen for an Azure DNS resource |
| `azure_image_reference` | A container or machine image a resource points at |
| `azure_resource_change` | One reported change to an Azure resource |
| `azure_collection_warning` | Part of the scope could not be read fully |

New to the words *fact* and *claim*? [Concepts](../concepts/README.md) defines them in plain language.
