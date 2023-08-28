package outline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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

	if res.JSON200 != nil {
		for _, document := range *res.JSON200.Data {
			deleteRes, err := c.Client.PostDocumentsDeleteWithResponse(context.Background(), PostDocumentsDeleteJSONRequestBody{
				Id: document.Id.String(),
			})
			if err != nil {
				return err
			}
			if deleteRes.StatusCode() != 200 {
				fmt.Println(string(deleteRes.Body))
				return fmt.Errorf("failed to delete document %s", document.Id.String())
			}
		}
	}

	draftsRes, err := c.Client.PostDocumentsDraftsWithResponse(context.Background(), PostDocumentsDraftsJSONRequestBody{
		CollectionId: &collectionId,
	})

	if err != nil {
		return err
	}

	if draftsRes.JSON200 != nil {
		for _, document := range *res.JSON200.Data {
			deleteRes, err := c.Client.PostDocumentsDeleteWithResponse(context.Background(), PostDocumentsDeleteJSONRequestBody{
				Id: document.Id.String(),
			})
			if err != nil {
				return err
			}
			if deleteRes.StatusCode() != 200 {
				fmt.Println(string(deleteRes.Body))
				return fmt.Errorf("failed to delete document %s", document.Id.String())
			}
		}
	}

	return nil

}

func (c *OutlineExtendedClient) CreateDocument(body PostDocumentsCreateJSONRequestBody) (*PostDocumentsCreateResponse, error) {
	var publish = true
	body.Publish = &publish
	return c.Client.PostDocumentsCreateWithResponse(context.Background(), body)
}

func (c *OutlineExtendedClient) ImportDocument(body PostDocumentsImportMultipartRequestBody, filename string, title string) (*PostDocumentsImportResponse, error) {

	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)

	collectionIdField, err := bodyWriter.CreateFormField("collectionId")
	if err != nil {
		return nil, err
	}
	_, err = collectionIdField.Write([]byte(body.CollectionId.String()))
	if err != nil {
		fmt.Println("Error writing JSON data:", err)
		return nil, err
	}

	if body.ParentDocumentId != nil {
		parentDocumentIdField, err := bodyWriter.CreateFormField("parentDocumentId")
		if err != nil {
			return nil, err
		}
		_, err = parentDocumentIdField.Write([]byte(body.ParentDocumentId.String()))
		if err != nil {
			fmt.Println("Error writing JSON data:", err)
			return nil, err
		}
	}

	titleField, err := bodyWriter.CreateFormField("title")
	if err != nil {
		return nil, err
	}
	_, err = titleField.Write([]byte(title))
	if err != nil {
		fmt.Println("Error writing JSON data:", err)
		return nil, err
	}

	publishField, err := bodyWriter.CreateFormField("publish")
	if err != nil {
		return nil, err
	}
	_, err = publishField.Write([]byte("true"))
	if err != nil {
		fmt.Println("Error writing JSON data:", err)
		return nil, err
	}

	// Add the file as a form file
	// Add the file as a form file with a custom Content-Type header
	fileWriter, err := bodyWriter.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename)},
		"Content-Type":        []string{"application/msword"},
	})
	if err != nil {
		fmt.Println("Error creating file field:", err)
		return nil, err
	}
	file, err := os.Open("tmp/" + filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return nil, err
	}
	defer file.Close()
	_, err = io.Copy(fileWriter, file)
	if err != nil {
		fmt.Println("Error copying file content:", err)
		return nil, err
	}

	// Close the multipart writer
	bodyWriter.Close()

	return c.Client.PostDocumentsImportWithBodyWithResponse(context.Background(), bodyWriter.FormDataContentType(), bodyBuf)
}
