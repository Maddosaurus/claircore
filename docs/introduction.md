# Claircore

Claircore is the engine behind the Clair v4 container security solution.
The Claircore package exports our domain models, interfaces necessary to plug into our business logic, and a default set of implementations.
This default set of implementations define our support matrix and consists of the following distributions and languages:

- Ubuntu
- Debian
- RHEL
- Red Hat Container First content
- SUSE
- Oracle
- Alpine
- AWS Linux
- VMWare Photon
- Python
- Java
- Go
- Ruby

Claircore relies on PostgreSQL for its persistence and the library will handle migrations if configured to do so.

The diagram below is a high level overview of Claircore's architecture. 

```mermaid
graph LR
subgraph Indexer
im[Image Manifest]
libindex[Libindex]
iir[IndexReport]
im --> libindex --> iir
end
iir -.-> db[(Database)]
```
```mermaid
graph LR
subgraph Matcher
mir[IndexReport]
libvuln[Libvuln]
vr[VulnerabilityReport]
mir --> libvuln --> vr
end
db[(Database)] -.-> mir
```

When a claircore.Manifest is submitted to Libindex, the library will index its constituent parts and create a report with its findings.

When a claircore.IndexReport is provided to Libvuln, the library will discover vulnerabilities affecting it and generate a claircore.VulnerabilityReport.
