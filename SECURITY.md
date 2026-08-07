# Security policy

Please report vulnerabilities privately through GitHub's security advisory
form for this repository. Do not include controller credentials, domain output,
or private network addresses in a public issue.

Only the latest released Rule-Bot Client version receives security fixes. Until the
first release is published, the default branch is the supported version.

Rule-Bot Client deliberately rejects HTTP redirects, ignores environment proxy
variables for controller traffic, and never exposes a listening port. These
controls reduce credential-leak and attack-surface risks; they do not make an
unencrypted remote controller connection confidential. Use HTTPS or a trusted
private network for controller access.
