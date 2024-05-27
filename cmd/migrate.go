/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	cf "github.com/essentialkaos/go-confluence/v6"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/spf13/cobra"
	"zzdats.lv/confluence-to-outline/confluence"
	"zzdats.lv/confluence-to-outline/outline"
)

type UrlInfo struct {
	NewUrl string
	DocId  string
}

type DocInfo struct {
	Count   int    `json:"Count"`
	Id      string `json:"DocumentID"`
	ConfURL string `json:"ConfluenceURL"`
	OutUrl  string `json:"OutlineURL"`
}

type Migrator struct {
	confluenceClient *confluence.ConfluenceExtendedClient
	outlineClient    *outline.OutlineExtendedClient
	urlMap           map[string]UrlInfo
	spaceKey         string
	collectionId     string
}

// migrateCmd represents the migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate confluence pages to outline documents",
	Long:  `Exports word documents from confluence space and uploads them to outline collection.`,
	Run: func(cmd *cobra.Command, args []string) {
		spaceKey, err := cmd.Flags().GetString("from")
		if err != nil {
			panic(err)
		}

		collectionId, err := cmd.Flags().GetString("to")
		if err != nil {
			panic(err)
		}

		outlineClient, err := outline.GetClient()
		if err != nil {
			panic(err)
		}

		collectionInfo, err := outlineClient.Client.PostCollectionsInfoWithResponse(context.Background(), outline.PostCollectionsInfoJSONRequestBody{
			Id: uuid.MustParse(collectionId),
		})
		if err != nil {
			panic(err)
		}
		collectionTitle := *collectionInfo.JSON200.Data.Name

		confluenceClient, err := confluence.GetClient()
		if err != nil {
			panic(err)
		}

		space, err := confluenceClient.Client.GetSpace(spaceKey, cf.SpaceParameters{
			SpaceKey: []string{spaceKey},
		})
		if err != nil {
			panic(err)
		}
		migrator := Migrator{
			confluenceClient: confluenceClient,
			outlineClient:    outlineClient,
			urlMap:           make(map[string]UrlInfo),
			spaceKey:         spaceKey,
			collectionId:     collectionId,
		}
		fmt.Printf("Migrating confluence pages from Confluence space \"%s\" (%s) to Outline collection \"%s\" (%s).\n", spaceKey, space.Name, collectionId, collectionTitle)

		rootPages, err := confluenceClient.Client.GetSpaceContent(spaceKey, cf.SpaceParameters{
			SpaceKey: []string{spaceKey},
			Expand:   []string{"version", "body.storage", "children.page"},
			Depth:    "1",
			Limit:    10,
		})
		if err != nil {
			panic(err)
		}
		for _, page := range rootPages.Pages.Results {
			migrator.migratePageRecurse(page, "")
		}
		migrator.saveUrlMapToFile() //Comment out to disable saving URL map to json file
		migrator.replaceUrls()

	},
}

func (m Migrator) replaceUrls() {
	publish := true
	appendDoc := false
	done := true
	confluenceHostname := strings.TrimSuffix(m.confluenceClient.GetBaseURL(), "/")
	outlineHostname := strings.TrimSuffix(m.outlineClient.GetBaseURL(), "/api")

	jsonCount := 0
	jsonCount2 := 0
	var checkURLs []DocInfo
	var checkStringJSON []DocInfo
	for _, urlInfo := range m.urlMap {
		resp, err := m.outlineClient.Client.PostDocumentsInfoWithResponse(context.Background(), outline.PostDocumentsInfoJSONRequestBody{
			Id: &urlInfo.DocId,
		})
		if err != nil {
			panic(err)
		}
		document := resp.JSON200
		replacedContent := *document.Data.Text
		for oldUrl, urlInfo2 := range m.urlMap {
			oldUrlWrapped := "(" + oldUrl + ")" //Relative URL
			newUrlWrapped := "(" + urlInfo2.NewUrl + ")"
			oldUrlHostnameWrapped := "(" + confluenceHostname + oldUrl + ")" //Absolute URL
			newUrlHostnameWrapped := "(" + outlineHostname + urlInfo2.NewUrl + ")"

			replacedContent = strings.ReplaceAll(replacedContent, oldUrlWrapped, newUrlWrapped)
			replacedContent = strings.ReplaceAll(replacedContent, oldUrlHostnameWrapped, newUrlHostnameWrapped)

			if strings.Contains(replacedContent, "\n]"+newUrlHostnameWrapped+"[") || strings.Contains(replacedContent, "\n]"+newUrlWrapped+"[") {
				for oldUrl2, urlInfoFromMap := range m.urlMap { //Getting the old confluence URL with only knowing the new outline URL
					if urlInfoFromMap.NewUrl == urlInfo.NewUrl {
						exists := false
						for _, docInfo := range checkURLs {
							if docInfo.Id == urlInfo.DocId {
								exists = true
								break
							}
						}
						if !exists {
							jsonCount++
							checkURLs = append(checkURLs, DocInfo{Id: urlInfo.DocId, OutUrl: urlInfo.NewUrl, ConfURL: oldUrl2, Count: jsonCount})
						}
						break
					}
				}
			} //Logging of URLs where relative or absolute URLs are weirdly formatted/wrong

		}
		stringToCheck, exists := os.LookupEnv("CHECK_STRING")
		if !exists {
			stringToCheck = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaak"
		} //Any string that shouldn't come up in any page works if you want no checking
		if strings.Contains(replacedContent, stringToCheck) {
			for oldUrl, urlInfoFromMap := range m.urlMap { //Getting the old confluence URL with only knowing the new outline URL
				if urlInfoFromMap.NewUrl == urlInfo.NewUrl {
					exists := false
					for _, docInfo := range checkStringJSON {
						if docInfo.Id == urlInfo.DocId {
							exists = true
							break
						}
					}
					if !exists {
						jsonCount2++
						checkStringJSON = append(checkStringJSON, DocInfo{Id: urlInfo.DocId, OutUrl: urlInfo.NewUrl, ConfURL: oldUrl, Count: jsonCount2})
					}
					break
				}
			}
		}

		_, err = m.outlineClient.Client.PostDocumentsUpdateWithResponse(context.Background(), outline.PostDocumentsUpdateJSONRequestBody{
			Id:      urlInfo.DocId,
			Title:   document.Data.Title,
			Text:    &replacedContent,
			Append:  &appendDoc,
			Publish: &publish,
			Done:    &done,
		})
		if err != nil {
			panic(err)
		}

	}
	outputJSON(checkURLs, "checkURLs")             //Comment out if you dont want json debug output files
	outputJSON(checkStringJSON, "checkStringJSON") //Comment out if you dont want json debug output files
}

func outputJSON(checkStruct []DocInfo, filename string) { //Formats structs to JSON with newlines
	file, err := os.Create(filename + ".json")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\n")
	err = encoder.Encode(checkStruct)
	if err != nil {
		panic(err)
	}
}

func (m Migrator) migratePageRecurse(page *cf.Content, parentDocumentId string) {
	var publish = true
	exportedDoc, err := m.confluenceClient.ExportDoc(page.ID)
	if err != nil {
		panic(err)
	}
	exportedDocBytes, err := os.ReadFile("tmp/" + *exportedDoc) // just pass the file name
	if err != nil {
		fmt.Print(err)
	}

	collectionUuid := uuid.MustParse(m.collectionId)
	importFileRequest := map[string]any{
		"file": exportedDocBytes,
	}
	importDocumentReq := outline.PostDocumentsImportMultipartRequestBody{
		CollectionId: &collectionUuid,

		// File Only plain text, markdown, docx, and html format are supported.
		File:    &importFileRequest,
		Publish: &publish,
	}
	if parentDocumentId != "" {
		parentDocumentUuid := uuid.MustParse(parentDocumentId)
		importDocumentReq.ParentDocumentId = &parentDocumentUuid
	}
	importDocumentRes, err := m.outlineClient.ImportDocument(importDocumentReq, *exportedDoc, page.Title)
	if err != nil {
		panic(err)
	}
	m.createPageMapping(page, importDocumentRes)

	// File import cleanup
	os.Remove("tmp/" + *exportedDoc)
	createdDocumentId := *importDocumentRes.JSON200.Data.Id                                                  //Temp cleanup
	fmt.Printf("Imported document \"%s\" (%s).\n", createdDocumentId, *importDocumentRes.JSON200.Data.Title) //Console logging

	if page.Children == nil || page.Children.Pages.Size == 0 {
		return
	}
	fmt.Printf("Migrating %d child pages of %s (%s).\n", page.Children.Pages.Size, page.ID, page.Title) //Console logging

	for _, childPage := range page.Children.Pages.Results {

		childPageFull, err := m.confluenceClient.Client.GetContentByID(childPage.ID, cf.ContentIDParameters{
			Expand: []string{"version", "body.storage", "children.page"},
		})
		if err != nil {
			panic(err)
		}
		m.migratePageRecurse(childPageFull, createdDocumentId.String())
	}
}

func (m *Migrator) createPageMapping(page *cf.Content, importDocumentRes *outline.PostDocumentsImportResponse) {
	createdDocumentId := *importDocumentRes.JSON200.Data.Id
	title := *importDocumentRes.JSON200.Data.Title
	urlId := *importDocumentRes.JSON200.Data.UrlId
	titleSlug := slug.Make(title)                         // Slug is not present for input document response
	newUrl := fmt.Sprintf(`/doc/%s-%s`, titleSlug, urlId) // Outline URL
	confluenceURLs := m.getPossibleConfluenceURLs(page)
	for i := range confluenceURLs {
		m.urlMap = updateUrlMap(m.urlMap, confluenceURLs[i], newUrl, createdDocumentId.String())
	}
}

func (m Migrator) getPossibleConfluenceURLs(page *cf.Content) []string {
	var urls []string
	urls = append(urls, fmt.Sprintf(`/pages/viewpage.action?pageId=%s`, page.ID))
	encodedTitle := strings.ReplaceAll(page.Title, ":", "%3A")
	urls = append(urls, fmt.Sprintf(`/display/%s/%s`, m.spaceKey, encodedTitle))
	encodedTitle = strings.ReplaceAll(encodedTitle, " ", "+")
	urls = append(urls, fmt.Sprintf(`/display/%s/%s`, m.spaceKey, encodedTitle))
	return urls
}

func updateUrlMap(urlMap map[string]UrlInfo, oldUrl, newUrl, createdDocumentId string) map[string]UrlInfo {
	urlMap[oldUrl] = UrlInfo{NewUrl: newUrl, DocId: createdDocumentId}
	return urlMap
}

func (m Migrator) saveUrlMapToFile() { //For debug purposes saves URL map to json file. Function call can be commented out.

	urlMapJSON, err := json.MarshalIndent(m.urlMap, "", " ")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(urlMapJSON))
	file, err := os.Create("urlMap.json")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	_, err = file.Write(urlMapJSON)
	if err != nil {
		panic(err)
	}
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.PersistentFlags().String("from", "", "Confluence SpaceKey to migrate pages from")
	migrateCmd.MarkPersistentFlagRequired("from")
	migrateCmd.PersistentFlags().String("to", "", "Outline collection id to import documents into")
	migrateCmd.MarkPersistentFlagRequired("to")
}
