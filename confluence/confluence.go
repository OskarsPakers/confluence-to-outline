package confluence

import (
	"fmt"
	"io"
	"net/http"
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
	Client  *cf.API
	baseUrl string
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
	api, err := cf.NewAPI(confluenceBaseUrl, AuthEmpty{})

	if err != nil {
		return nil, err
	}
	return &ConfluenceExtendedClient{
		Client:  api,
		baseUrl: confluenceBaseUrl}, nil
}

func (c *ConfluenceExtendedClient) GetBaseURL() string {
	return c.baseUrl
}

func (c *ConfluenceExtendedClient) ExportDoc(pageId string) (*string, error) {
	// return c.Client.url
	// "CONFLUENCE_URL" + "exportword?pageId=PAGE_ID"
	filename := pageId + ".doc"
	out, err := os.Create("tmp/" + pageId + ".doc")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	resp, err := http.Get(c.baseUrl + "/exportword?pageId=" + pageId)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		panic(err)
	}
	return &filename, nil
}
