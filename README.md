# Confluence to Outline

Command line tool to migrate Confluence pages to Outline.


```
Command line tool to migrate confluence pages with attachments and tree structure to outline documents.

Usage:
  confluence-to-outline [command]

Available Commands:
  clean       Delete all documents in collection
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  migrate     Migrate confluence pages to outline documents

Flags:
  -h, --help   help for confluence-to-outline

Use "confluence-to-outline [command] --help" for more information about a command.
```

## Generating Outline API client

Outline does not publish Go client, but one can be generated.
1. Download Outline API spec from https://raw.githubusercontent.com/outline/openapi/main/spec3.yml
2. Generate API client using deepmap/oapi-codegen

```
go install github.com/deepmap/oapi-codegen/cmd/oapi-codegen@latest
cd outline
oapi-codegen -package outline -config outline_codegen_config.yml outline_openapi_spec3.yml > outline.gen.go
```
