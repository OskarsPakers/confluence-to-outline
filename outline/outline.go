package outline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type OutlineExtendedClient struct {
	Client         *ClientWithResponses
	outlineBaseUrl string
	logger         *slog.Logger
}

func GetClient(logger *slog.Logger) (*OutlineExtendedClient, error) {
	err := godotenv.Load()
	if err != nil {
		logger.Info(".env file not loaded, reading OUTLINE_API_TOKEN and OUTLINE_BASE_URL from env variables.")
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
		logger.Error("Error", err)
		return nil, err
	}
	return &OutlineExtendedClient{Client: client, logger: logger}, nil
}

func (c *OutlineExtendedClient) GetBaseURL() string {
	return c.outlineBaseUrl
}

func (c *OutlineExtendedClient) CleanCollection(collection string) error {
	var collectionId = uuid.MustParse(collection)
	c.logger.Info("Cleaning collection", "Collection", collectionId)
	res, err := c.Client.PostDocumentsListWithResponse(context.Background(), PostDocumentsListJSONRequestBody{
		CollectionId: &collectionId,
	})
	if err != nil {
		c.logger.Error("Error", err)
		return err
	}

	if res.JSON200 != nil {
		for _, document := range *res.JSON200.Data {
			deleteRes, err := c.Client.PostDocumentsDeleteWithResponse(context.Background(), PostDocumentsDeleteJSONRequestBody{
				Id: document.Id.String(),
			})
			c.logger.Debug("Clean info", "StatusCode", string(rune(deleteRes.StatusCode())), "ResponseBody", string(deleteRes.Body), "DocumentId", document.Id.String(), "DocumentTitle", string(*document.Title)) //TODO remove after clean fixed
			if err != nil {
				c.logger.Error("Error", err)
				return err
			}
			if deleteRes.StatusCode() != 200 {
				c.logger.Debug(string(deleteRes.Body))
				c.logger.Error("Failed to delete document", "Document", document.Id.String())
				return fmt.Errorf("failed to delete document %s", document.Id.String())
			}
		}
	}

	draftsRes, err := c.Client.PostDocumentsDraftsWithResponse(context.Background(), PostDocumentsDraftsJSONRequestBody{
		CollectionId: &collectionId,
	})

	if err != nil {
		c.logger.Error("Error", err)
		return err
	}

	if draftsRes.JSON200 != nil {
		for _, document := range *res.JSON200.Data {
			deleteRes, err := c.Client.PostDocumentsDeleteWithResponse(context.Background(), PostDocumentsDeleteJSONRequestBody{
				Id: document.Id.String(),
			})
			if err != nil {
				c.logger.Error("Error", err)
				return err
			}
			if deleteRes.StatusCode() != 200 {
				c.logger.Debug(string(deleteRes.Body))
				c.logger.Error("Failed to delete document", "Document", document.Id.String())
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
		c.logger.Error("Error", err)
		return nil, err
	}
	_, err = collectionIdField.Write([]byte(body.CollectionId.String()))
	if err != nil {
		c.logger.Error("Error writing JSON data:", err)
		return nil, err
	}

	if body.ParentDocumentId != nil {
		parentDocumentIdField, err := bodyWriter.CreateFormField("parentDocumentId")
		if err != nil {
			c.logger.Error("Error", err)
			return nil, err
		}
		_, err = parentDocumentIdField.Write([]byte(body.ParentDocumentId.String()))
		if err != nil {
			c.logger.Error("Error writing JSON data:", err)
			return nil, err
		}
	}

	titleField, err := bodyWriter.CreateFormField("title")
	if err != nil {
		c.logger.Error("Error", err)
		return nil, err
	}
	_, err = titleField.Write([]byte(title))
	if err != nil {
		c.logger.Error("Error writing JSON data:", err)
		return nil, err
	}

	publishField, err := bodyWriter.CreateFormField("publish")
	if err != nil {
		c.logger.Error("Error", err)
		return nil, err
	}
	_, err = publishField.Write([]byte("true"))
	if err != nil {
		c.logger.Error("Error writing JSON data:", err)
		return nil, err
	}

	// Add the file as a form file
	// Add the file as a form file with a custom Content-Type header
	fileWriter, err := bodyWriter.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename)},
		"Content-Type":        []string{"application/msword"},
	})
	if err != nil {
		c.logger.Error("Error creating file field:", err)
		return nil, err
	}
	file, err := os.Open("tmp/" + filename)
	if err != nil {
		c.logger.Error("Error opening file:", err)
		return nil, err
	}
	defer file.Close()
	_, err = io.Copy(fileWriter, file)
	if err != nil {
		c.logger.Error("Error copying file content:", err)
		return nil, err
	}

	// Close the multipart writer
	bodyWriter.Close()

	return c.Client.PostDocumentsImportWithBodyWithResponse(context.Background(), bodyWriter.FormDataContentType(), bodyBuf)
}
