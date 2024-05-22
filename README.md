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

## Usage

An .env file is required with the following fields -
```
CONFLUENCE_BASE_URL=https://hostname.com/
OUTLINE_API_TOKEN={api_token}
OUTLINE_BASE_URL=http://127.0.0.1:8888/api
```
The Confluence base URL must end with /  
Outline base URL must end with /api  
Outline API token is available in Account settings - API Tokens

### Migration -
```
go run main.go migrate --from {Confluence SpaceKey} --to {Outline collection ID}
```
**SpaceKey** -  
The all capital letter keyword after /display/ part of the URL  
'hostname/display/TEST/Test' has the SpaceKey "TEST"  

**Outline CollectionID** -  
While in the inspect element Network tab in the destination Outline page Star the target collection.  
There will be a POST request with the collectionId in the Request and Response tabs. 

### Clean -
```
go run main.go clean --collection {Outline collection ID}
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
