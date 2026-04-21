package outline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"golang.org/x/time/rate"
)

// rateLimitedDoer wraps an http.Client with a token bucket limiter so the
// combined request rate across all Outline API calls stays below the server's
// RATE_LIMITER_REQUESTS / RATE_LIMITER_DURATION_WINDOW setting.
type rateLimitedDoer struct {
	client  *http.Client
	limiter *rate.Limiter
}

func (d *rateLimitedDoer) Do(req *http.Request) (*http.Response, error) {
	if d.limiter != nil {
		ctx := req.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := d.limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	return d.client.Do(req)
}

type OutlineExtendedClient struct {
	Client         *ClientWithResponses
	httpDoer       HttpRequestDoer
	outlineBaseUrl string
	logger         *slog.Logger
}

// RateLimit configures throttling of outbound Outline API requests. It mirrors
// Outline's RATE_LIMITER_REQUESTS / RATE_LIMITER_DURATION_WINDOW settings.
// A Requests value <= 0 disables throttling.
type RateLimit struct {
	Requests int
	Window   time.Duration
}

func GetClient(logger *slog.Logger, rateLimit RateLimit) (*OutlineExtendedClient, error) {
	err := godotenv.Load()
	if err != nil {
		logger.Info(".env file not loaded, reading OUTLINE_API_TOKEN and OUTLINE_BASE_URL from env variables.")
	}
	apiToken := os.Getenv("OUTLINE_API_TOKEN")

	if apiToken == "" {
		fmt.Fprintln(os.Stderr, "OUTLINE_API_TOKEN is not set")
		os.Exit(1)
	}
	outlineBaseUrl := os.Getenv("OUTLINE_BASE_URL")

	if outlineBaseUrl == "" {
		fmt.Fprintln(os.Stderr, "OUTLINE_BASE_URL is not set")
		os.Exit(1)
	}

	var doer HttpRequestDoer = &http.Client{}
	if rateLimit.Requests > 0 && rateLimit.Window > 0 {
		// Burst = 1 enforces strict pacing (one request per 1/rate seconds).
		// A larger burst would let the client fire many requests instantly and
		// immediately fill Outline's sliding window, tripping the 429 limit
		// even though our long-term rate is correct.
		perSecond := float64(rateLimit.Requests) / rateLimit.Window.Seconds()
		doer = &rateLimitedDoer{
			client:  &http.Client{},
			limiter: rate.NewLimiter(rate.Limit(perSecond), 1),
		}
		logger.Info("Outline request throttling enabled", "requests", rateLimit.Requests, "windowSeconds", rateLimit.Window.Seconds(), "minIntervalMs", int(1000.0/perSecond))
	}

	client, err := NewClientWithResponses(outlineBaseUrl,
		WithHTTPClient(doer),
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
	return &OutlineExtendedClient{Client: client, httpDoer: doer, outlineBaseUrl: outlineBaseUrl, logger: logger}, nil
}

func (c *OutlineExtendedClient) GetBaseURL() string {
	return c.outlineBaseUrl
}

// UploadAttachment uploads image data to Outline using its two-step S3 pre-signed upload.
// Returns the public URL of the uploaded attachment.
func (c *OutlineExtendedClient) UploadAttachment(imageData []byte, filename string, contentType string) (string, error) {
	// Step 1: Request a pre-signed upload URL from Outline.
	// Build request manually to avoid the generated struct's float32 size field
	// which may not match the API response (Outline returns size as string).
	reqBodyBytes, err := json.Marshal(map[string]interface{}{
		"name":        filename,
		"contentType": contentType,
		"size":        len(imageData),
	})
	if err != nil {
		return "", err
	}

	httpReq, err := NewPostAttachmentsCreateRequestWithBody(
		c.Client.ClientInterface.(*Client).Server,
		"application/json",
		bytes.NewReader(reqBodyBytes),
	)
	if err != nil {
		return "", err
	}

	// Apply auth editors (adds Authorization header)
	if err := c.Client.ClientInterface.(*Client).applyEditors(context.Background(), httpReq, nil); err != nil {
		return "", err
	}

	httpResp, err := c.Client.ClientInterface.(*Client).Client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()

	var createResult struct {
		Data struct {
			UploadUrl  string                 `json:"uploadUrl"`
			Form       map[string]interface{} `json:"form"`
			Attachment struct {
				Url string `json:"url"`
			} `json:"attachment"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&createResult); err != nil {
		return "", fmt.Errorf("attachments.create parse error: %w", err)
	}
	if createResult.Data.UploadUrl == "" {
		return "", fmt.Errorf("attachments.create returned no uploadUrl (status %d)", httpResp.StatusCode)
	}

	// Resolve relative uploadURL (e.g. /api/files.create) against the Outline base URL
	uploadURL := createResult.Data.UploadUrl
	if !strings.HasPrefix(uploadURL, "http://") && !strings.HasPrefix(uploadURL, "https://") {
		base := strings.TrimSuffix(c.outlineBaseUrl, "/api")
		base = strings.TrimSuffix(base, "/")
		uploadURL = base + "/" + strings.TrimPrefix(uploadURL, "/")
	}
	formFields := createResult.Data.Form
	attachmentURL := createResult.Data.Attachment.Url

	// Step 2: POST the file to the pre-signed S3 URL
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)

	// S3 requires all policy fields before the file
	for key, val := range formFields {
		if err := bodyWriter.WriteField(key, fmt.Sprintf("%v", val)); err != nil {
			return "", err
		}
	}
	fw, err := bodyWriter.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err = fw.Write(imageData); err != nil {
		return "", err
	}
	bodyWriter.Close()

	req, err := http.NewRequest(http.MethodPost, uploadURL, bodyBuf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", bodyWriter.FormDataContentType())

	// If uploadURL points to Outline itself (not an external S3), apply auth
	// and route through the rate-limited doer so uploads count against the limit.
	outlineHost := strings.TrimSuffix(c.outlineBaseUrl, "/api")
	outlineHost = strings.TrimSuffix(outlineHost, "/")
	uploadDoer := HttpRequestDoer(http.DefaultClient)
	if strings.HasPrefix(uploadURL, outlineHost) {
		if err := c.Client.ClientInterface.(*Client).applyEditors(context.Background(), req, nil); err != nil {
			return "", err
		}
		uploadDoer = c.httpDoer
	}

	resp, err := uploadDoer.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("attachment upload failed: status %d: %s", resp.StatusCode, string(body))
	}

	return attachmentURL, nil
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
		"Content-Type":        []string{"text/html"},
	})
	if err != nil {
		c.logger.Error("Error creating file field:", err)
		return nil, err
	}
	file, err := os.Open("export/" + filename)
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
