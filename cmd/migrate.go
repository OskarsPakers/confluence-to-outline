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
	"sync"

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

var urlMap = make(map[string]UrlInfo)
var urlMapMutex = &sync.Mutex{}

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
			migratePageRecurse(page, "", confluenceClient, outlineClient, collectionId, nil, spaceKey)
		}
		saveUrlMapToFile() //Comment out to disable saving URL map to json file
		replaceUrls(outlineClient)

	},
}

func replaceUrls(outlineClient *outline.OutlineExtendedClient) {
	urlMapMutex.Lock()
	defer urlMapMutex.Unlock()
	publish := true
	appendDoc := false
	done := true
	confluenceHostname := strings.TrimSuffix(os.Getenv("CONFLUENCE_BASE_URL"), "/")
	outlineHostname := strings.TrimSuffix(os.Getenv("OUTLINE_BASE_URL"), "/api")

	jsonCount := 0
	jsonCount2 := 0
	var checkURLs []DocInfo
	var checkStringJSON []DocInfo
	for _, urlInfo := range urlMap {
		resp, err := outlineClient.Client.PostDocumentsInfoWithResponse(context.Background(), outline.PostDocumentsInfoJSONRequestBody{
			Id: &urlInfo.DocId,
		})
		if err != nil {
			panic(err)
		}
		document := resp.JSON200
		replacedContent := *document.Data.Text
		for oldUrl, urlInfo2 := range urlMap {
			oldUrlWrapped := "(" + oldUrl + ")" //Relative URL
			newUrlWrapped := "(" + urlInfo2.NewUrl + ")"
			oldUrlHostnameWrapped := "(" + confluenceHostname + oldUrl + ")" //Absolute URL
			newUrlHostnameWrapped := "(" + outlineHostname + urlInfo2.NewUrl + ")"

			replacedContent = strings.ReplaceAll(replacedContent, oldUrlWrapped, newUrlWrapped)
			replacedContent = strings.ReplaceAll(replacedContent, oldUrlHostnameWrapped, newUrlHostnameWrapped)

			if strings.Contains(replacedContent, "\n]"+newUrlHostnameWrapped+"[") || strings.Contains(replacedContent, "\n]"+newUrlWrapped+"[") {
				for oldUrl2, urlInfoFromMap := range urlMap { //Getting the old confluence URL with only knowing the new outline URL
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
			for oldUrl, urlInfoFromMap := range urlMap { //Getting the old confluence URL with only knowing the new outline URL
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

		_, err = outlineClient.Client.PostDocumentsUpdateWithResponse(context.Background(), outline.PostDocumentsUpdateJSONRequestBody{
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

func migratePageRecurse(page *cf.Content, parentDocumentId string, confluenceClient *confluence.ConfluenceExtendedClient, outlineClient *outline.OutlineExtendedClient, collectionId string, rootDocumentId *uuid.UUID, spacekey string) {
	var publish = true
	exportedDoc, err := confluenceClient.ExportDoc(page.ID)
	if err != nil {
		panic(err)
	}
	exportedDocBytes, err := os.ReadFile("tmp/" + *exportedDoc) // just pass the file name
	if err != nil {
		fmt.Print(err)
	}

	collectionUuid := uuid.MustParse(collectionId)
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
	importDocumentRes, err := outlineClient.ImportDocument(importDocumentReq, *exportedDoc, page.Title)
	if err != nil {
		panic(err)
	}
	createdDocumentId := *importDocumentRes.JSON200.Data.Id

	// URL Mapping for variations of Confluence URLs to Outline URLs
	title := *importDocumentRes.JSON200.Data.Title
	urlId := *importDocumentRes.JSON200.Data.UrlId
	titleSlug := slug.Make(title)
	newUrl := fmt.Sprintf(`/doc/%s-%s`, titleSlug, urlId)              // Outline URL
	oldUrl := fmt.Sprintf(`/pages/viewpage.action?pageId=%s`, page.ID) // Confluence URL
	updateUrlMap(urlMap, oldUrl, newUrl, createdDocumentId.String(), urlMapMutex)

	urlwords := strings.Split(page.Title, " ")
	for i, word := range urlwords {
		urlwords[i] = strings.ReplaceAll(word, ":", "%3A")
	}

	if len(urlwords) == 1 {
		oldUrl = fmt.Sprintf(`/display/%s/%s`, spacekey, urlwords[0])
	} else {
		oldUrl = fmt.Sprintf(`/display/%s/%s`, spacekey, strings.Join(urlwords, "+"))
	}
	updateUrlMap(urlMap, oldUrl, newUrl, createdDocumentId.String(), urlMapMutex)

	oldUrl = fmt.Sprintf(`/display/%s/%s`, spacekey, strings.Join(urlwords, " "))
	updateUrlMap(urlMap, oldUrl, newUrl, createdDocumentId.String(), urlMapMutex)

	// File import cleanup
	os.Remove("tmp/" + *exportedDoc)                                                                         //Temp cleanup
	fmt.Printf("Imported document \"%s\" (%s).\n", createdDocumentId, *importDocumentRes.JSON200.Data.Title) //Console logging

	if page.Children == nil || page.Children.Pages.Size == 0 {
		return
	}
	fmt.Printf("Migrating %d child pages of %s (%s).\n", page.Children.Pages.Size, page.ID, page.Title) //Console logging

	for _, childPage := range page.Children.Pages.Results {

		childPageFull, err := confluenceClient.Client.GetContentByID(childPage.ID, cf.ContentIDParameters{
			Expand: []string{"version", "body.storage", "children.page"},
		})
		if err != nil {
			panic(err)
		}
		migratePageRecurse(childPageFull, createdDocumentId.String(), confluenceClient, outlineClient, collectionId, rootDocumentId, spacekey)
	}
}

func updateUrlMap(urlMap map[string]UrlInfo, oldUrl, newUrl, createdDocumentId string, urlMapMutex *sync.Mutex) {
	urlMapMutex.Lock()
	urlMap[oldUrl] = UrlInfo{NewUrl: newUrl, DocId: createdDocumentId}
	urlMapMutex.Unlock()
}

func saveUrlMapToFile() { //For debug purposes saves URL map to json file. Function call can be commented out.
	urlMapMutex.Lock()
	defer urlMapMutex.Unlock()

	file, err := os.Create("urlMap.json")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	for key, value := range urlMap {
		line := fmt.Sprintf("%s: %s\n", key, value)
		_, err := file.WriteString(line)
		if err != nil {
			panic(err)
		}
	}
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.PersistentFlags().String("from", "", "Confluence SpaceKey to migrate pages from")
	migrateCmd.MarkPersistentFlagRequired("from")
	migrateCmd.PersistentFlags().String("to", "", "Outline collection id to import documents into")
	migrateCmd.MarkPersistentFlagRequired("to")
}
