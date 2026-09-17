# Managed LazyCat applications

Each directory below is a complete LazyCat packaging project managed by this
repository. The upstream application repository only needs to publish a public
OCI image; it does not need LazyCat manifests, credentials, or workflows.

`poster/` monitors `ghcr.io/wcaqrl/poster`, copies a new stable image into the
official LazyCat registry, builds the LPK, creates a release asset, and submits
the application version for review.
