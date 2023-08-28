package outline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type OutlineExtendedClient struct {
	Client *ClientWithResponses
}

func GetClient() (*OutlineExtendedClient, error) {
	err := godotenv.Load()
	if err != nil {
		fmt.Println(".env file not loaded, reading OUTLINE_API_TOKEN and OUTLINE_BASE_URL from env variables.")
	}
	apiToken := os.Getenv("OUTLINE_API_TOKEN")

	if apiToken == "" {
		panic("OUTLINE_API_TOKEN is not set")
	}
	outlineBaseUrl := os.Getenv("OUTLINE_BASE_URL")
	if outlineBaseUrl == "" {
		panic("OUTLINE_BASE_URL is not set")
	}
	client, err := NewClientWithResponses(outlineBaseUrl,
		WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			// add authorization header
			req.Header.Set("Authorization", "Bearer "+apiToken)
			return nil
		}),
	)

	if err != nil {
		return nil, err
	}
	return &OutlineExtendedClient{Client: client}, nil
}

func (c *OutlineExtendedClient) CleanCollection(collection string) error {
	var collectionId = uuid.MustParse(collection)
	res, err := c.Client.PostDocumentsListWithResponse(context.Background(), PostDocumentsListJSONRequestBody{
		CollectionId: &collectionId,
	})

	if err != nil {
		return err
	}

	if res.JSON200 == nil {
		fmt.Println("Collection is empty")
		return nil
	}

	for _, document := range *res.JSON200.Data {
		deleteRes, err := c.Client.PostDocumentsDeleteWithResponse(context.Background(), PostDocumentsDeleteJSONRequestBody{
			Id: document.Id.String(),
		})
		if err != nil {
			return err
		}
		if deleteRes.StatusCode() != 200 {
			return fmt.Errorf("failed to delete document %s", document.Id.String())
		}
	}
	return nil

}

func (c *OutlineExtendedClient) CreateDocument(body PostDocumentsCreateJSONRequestBody) (*PostDocumentsCreateResponse, error) {
	var publish = true
	body.Publish = &publish
	return c.Client.PostDocumentsCreateWithResponse(context.Background(), body)
}

func (c *OutlineExtendedClient) ImportDocument(body PostDocumentsImportMultipartRequestBody) (*PostDocumentsImportResponse, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return c.Client.PostDocumentsImportWithBodyWithResponse(context.Background(), "application/json", bodyReader)
}
