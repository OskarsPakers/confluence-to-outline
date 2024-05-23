/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
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
			migratePageRecurse(page, "", confluenceClient, outlineClient, collectionId)
		}
		saveUrlMapToFile() //Comment out to disable saving URL map to json file
		replaceUrls(outlineClient)

	},
}

func replaceUrls(outlineClient *outline.OutlineExtendedClient) {
	urlMapMutex.Lock()
	defer urlMapMutex.Unlock()
	publish := true
	append := false
	done := true
	for _, urlInfo := range urlMap {
		resp, err := outlineClient.Client.PostDocumentsInfoWithResponse(context.Background(), outline.PostDocumentsInfoJSONRequestBody{
			Id: &urlInfo.DocId,
		})
		if err != nil {
			panic(err)
		}
		document := resp.JSON200
		replacedContent := *document.Data.Text
		for oldUrl, urlInfo := range urlMap {
			oldUrlWrapped := "(" + oldUrl + ")"
			newUrlWrapped := "(" + urlInfo.NewUrl + ")"
			replacedContent = strings.ReplaceAll(replacedContent, oldUrlWrapped, newUrlWrapped)
		}
		_, err = outlineClient.Client.PostDocumentsUpdateWithResponse(context.Background(), outline.PostDocumentsUpdateJSONRequestBody{
			Id:      urlInfo.DocId,
			Title:   document.Data.Title,
			Text:    &replacedContent,
			Append:  &append,
			Publish: &publish,
			Done:    &done,
		})
		if err != nil {
			panic(err)
		}
	}
}

func migratePageRecurse(page *cf.Content, parentDocumentId string, confluenceClient *confluence.ConfluenceExtendedClient, outlineClient *outline.OutlineExtendedClient, collectionId string) {
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

	createdDocumentId := importDocumentRes.JSON200.Data.Id

	title := *importDocumentRes.JSON200.Data.Title
	urlId := *importDocumentRes.JSON200.Data.UrlId
	titleSlug := slug.Make(title)
	newUrl := fmt.Sprintf(`/doc/%s-%s`, titleSlug, urlId)
	oldUrl := fmt.Sprintf(`/pages/viewpage.action?pageId=%s`, page.ID)

	urlMapMutex.Lock()
	urlMap[oldUrl] = UrlInfo{NewUrl: newUrl, DocId: createdDocumentId.String()}
	urlMapMutex.Unlock()

	os.Remove("tmp/" + *exportedDoc)
	fmt.Printf("Imported document \"%s\" (%s).\n", createdDocumentId, *importDocumentRes.JSON200.Data.Title)

	if page.Children == nil || page.Children.Pages.Size == 0 {
		return
	}
	fmt.Printf("Migrating %d child pages of %s (%s).\n", page.Children.Pages.Size, page.ID, page.Title)

	for _, childPage := range page.Children.Pages.Results {

		childPageFull, err := confluenceClient.Client.GetContentByID(childPage.ID, cf.ContentIDParameters{
			Expand: []string{"version", "body.storage", "children.page"},
		})
		if err != nil {
			panic(err)
		}
		migratePageRecurse(childPageFull, createdDocumentId.String(), confluenceClient, outlineClient, collectionId)
	}
}

func saveUrlMapToFile() {
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
