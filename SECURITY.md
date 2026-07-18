# Security

Please report suspected vulnerabilities privately through GitHub's security
advisory feature rather than a public issue.

The Ollama assistant is optional. Its local inspection tools are read-only and
restricted to the active terminal directory; web-page fetching rejects
localhost and private-network targets. Tool output is still supplied to the
configured model, so do not enable AI tools in directories containing secrets
you do not want the model to process.

OSC 52 clipboard reads are disabled by default because terminal applications
could otherwise extract clipboard contents.

