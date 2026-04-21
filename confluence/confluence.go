package confluence

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"os"

	cf "github.com/essentialkaos/go-confluence/v6"
	"github.com/joho/godotenv"
)

type AuthEmpty struct {
}

func (a AuthEmpty) Validate() error {
	return nil
}

func (a AuthEmpty) Encode() string {
	return ""
}

type ConfluenceExtendedClient struct {
	Client   *cf.API
	baseUrl  string
	username string
	apiToken string
}

func GetClient() (*ConfluenceExtendedClient, error) {
	err := godotenv.Load()
	if err != nil {
		fmt.Println(".env file not loaded, reading CONFLUENCE_BASE_URL from env variables.")
	}
	confluenceBaseUrl := os.Getenv("CONFLUENCE_BASE_URL")
	if confluenceBaseUrl == "" {
		panic("CONFLUENCE_BASE_URL is not set")
	}
	username := os.Getenv("CONFLUENCE_USERNAME")
	apiToken := os.Getenv("CONFLUENCE_API_TOKEN")
	var auth cf.Auth
	if username != "" && apiToken != "" {
		auth = cf.AuthBasic{User: username, Password: apiToken}
	} else {
		auth = AuthEmpty{}
	}
	api, err := cf.NewAPI(confluenceBaseUrl, auth)

	if err != nil {
		return nil, err
	}
	return &ConfluenceExtendedClient{
		Client:   api,
		baseUrl:  confluenceBaseUrl,
		username: username,
		apiToken: apiToken,
	}, nil
}

func (c *ConfluenceExtendedClient) GetBaseURL() string {
	return c.baseUrl
}

type confluencePageResponse struct {
	Body struct {
		ExportView struct {
			Value string `json:"value"`
		} `json:"export_view"`
	} `json:"body"`
	Title string `json:"title"`
}

func (c *ConfluenceExtendedClient) ExportDoc(pageId string) (*string, error) {
	filename := pageId + ".html"

	url := c.baseUrl + "/rest/api/content/" + pageId + "?expand=body.export_view"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pageResp confluencePageResponse
	if err := json.NewDecoder(resp.Body).Decode(&pageResp); err != nil {
		return nil, err
	}

	escapedTitle := html.EscapeString(pageResp.Title)
	htmlContent := fmt.Sprintf("<html><head><meta charset=\"utf-8\"><title>%s</title></head><body><h1>%s</h1>%s</body></html>",
		escapedTitle,
		escapedTitle,
		pageResp.Body.ExportView.Value,
	)
	if err := os.MkdirAll("export", 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile("export/"+filename, []byte(htmlContent), 0644); err != nil {
		return nil, err
	}

	return &filename, nil
}

func (c *ConfluenceExtendedClient) DownloadImage(imageUrl string) ([]byte, string, error) {
	// Use a client that follows redirects but only sends Basic Auth to the Confluence host.
	// Confluence attachment URLs redirect to api.media.atlassian.com (JWT in URL, no auth needed).
	parsedBase, _ := url.Parse(c.baseUrl)
	confluenceHost := parsedBase.Host
	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Host != confluenceHost {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", imageUrl, nil)
	if err != nil {
		return nil, "", err
	}
	// Only set basic auth for Confluence URLs; media.atlassian.com uses JWT in URL
	if strings.Contains(imageUrl, confluenceHost) {
		req.SetBasicAuth(c.username, c.apiToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("failed to download image %s: status %d", imageUrl, resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}
	// Strip parameters (e.g. "; charset=UTF-8") — S3 policy requires exact MIME type match
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	return data, contentType, nil
}
